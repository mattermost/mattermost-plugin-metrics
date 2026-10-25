// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"container/list"
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/index"
	"golang.org/x/sync/singleflight"

	"github.com/mattermost/mattermost/server/v8/platform/shared/filestore"
)

// remoteBlockStore makes the tsdb blocks in the file store queryable. It keeps an index of
// the block metas in the file store and downloads the blocks that the queries need into a
// local cache, which is bounded in size and evicts the least recently used blocks.
type remoteBlockStore struct {
	backend   filestore.FileBackend
	remoteDir string
	cacheDir  string
	maxBytes  int64
	logger    log.Logger

	group     singleflight.Group
	downloads chan struct{} // limits the concurrent downloads
	wg        sync.WaitGroup
	seq       atomic.Int64

	mu     sync.Mutex
	metas  map[ulid.ULID]tsdb.BlockMeta
	blocks map[ulid.ULID]*cachedBlock
	lru    *list.List // of *cachedBlock, most recently used first
	failed map[ulid.ULID]time.Time
	wanted map[ulid.ULID]int // blocks that a fetch is waiting for, they are not evicted
	size   int64
	closed bool
}

type cachedBlock struct {
	id    ulid.ULID
	dir   string
	block *tsdb.Block
	size  int64
	refs  int
	elem  *list.Element
	// dropped blocks are no longer in the file store, they are removed once unused
	dropped bool
}

// remoteBlockCacheDir returns the cache directory for the file store blocks. It is kept
// outside of the tsdb directory since the tsdb would otherwise load or delete the blocks.
func remoteBlockCacheDir(dbPath string) string {
	return filepath.Clean(dbPath) + "-remote-cache"
}

func newRemoteBlockStore(backend filestore.FileBackend, remoteDir, cacheDir string, maxBytes int64, logger log.Logger) (*remoteBlockStore, error) {
	// blocks downloaded by a previous run are not tracked, start with an empty cache
	if err := os.RemoveAll(cacheDir); err != nil {
		return nil, fmt.Errorf("could not clean the remote block cache: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return nil, fmt.Errorf("could not create the remote block cache: %w", err)
	}

	return &remoteBlockStore{
		backend:   backend,
		remoteDir: remoteDir,
		cacheDir:  cacheDir,
		maxBytes:  maxBytes,
		logger:    logger,
		downloads: make(chan struct{}, remoteBlockMaxDownloads),
		metas:     make(map[ulid.ULID]tsdb.BlockMeta),
		blocks:    make(map[ulid.ULID]*cachedBlock),
		lru:       list.New(),
		failed:    make(map[ulid.ULID]time.Time),
		wanted:    make(map[ulid.ULID]int),
	}, nil
}

// refresh updates the index of the blocks in the file store. The metas of the blocks that are
// already known are not read again since the blocks are immutable.
func (s *remoteBlockStore) refresh() error {
	dirs, err := s.backend.ListDirectory(s.remoteDir)
	if err != nil {
		return fmt.Errorf("could not list the blocks in the file store: %w", err)
	}

	s.mu.Lock()
	known := make(map[ulid.ULID]tsdb.BlockMeta, len(s.metas))
	for id, m := range s.metas {
		known[id] = m
	}
	s.mu.Unlock()

	metas := make(map[ulid.ULID]tsdb.BlockMeta, len(dirs))
	for _, dir := range dirs {
		id, parseErr := ulid.Parse(filepath.Base(dir))
		if parseErr != nil {
			continue
		}
		if m, ok := known[id]; ok {
			metas[id] = m
			continue
		}

		// the meta is uploaded after the index and chunks, so a listed meta means that
		// the block is readable
		m, metaErr := readBlockMeta(filepath.Join(dir, metaFileName), s.backend.ReadFile)
		if metaErr != nil {
			level.Debug(s.logger).Log("msg", "unable to read meta file", "block", id.String(), "err", metaErr)
			continue
		}
		if m.ULID != id {
			level.Warn(s.logger).Log("msg", "block meta does not match its directory", "block", id.String())
			continue
		}
		metas[id] = *m
	}

	var unused []*cachedBlock
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.metas = metas
	for id, b := range s.blocks {
		if _, ok := metas[id]; ok {
			continue
		}
		// the block was removed from the file store, drop it from the cache too
		b.dropped = true
		if b.refs == 0 {
			unused = append(unused, s.removeLocked(b))
		}
	}
	for id := range s.failed {
		if _, ok := metas[id]; !ok {
			delete(s.failed, id)
		}
	}
	s.mu.Unlock()

	s.closeBlocks(unused)
	return nil
}

// minTime returns the min time of the oldest block in the file store.
func (s *remoteBlockStore) minTime() (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	minT := int64(math.MaxInt64)
	for id, m := range s.metas {
		if _, ok := s.failed[id]; ok {
			continue
		}
		minT = min(minT, m.MinTime)
	}
	return minT, minT != math.MaxInt64
}

// selectBlocks returns the blocks in the file store that overlap [mint, maxt] and are not
// covered by a local block or by another block in the file store.
//
// The file store holds the blocks of every node that was scraping, and local compaction
// uploads the compacted blocks next to their sources. A block is covered by another block
// when all of its sources are part of the other block. Blocks of different nodes do not
// share sources, so they are all selected and merged at query time.
func (s *remoteBlockStore) selectBlocks(mint, maxt int64, local []tsdb.BlockMeta) []ulid.ULID {
	s.mu.Lock()
	defer s.mu.Unlock()

	type candidate struct {
		meta    tsdb.BlockMeta
		sources map[ulid.ULID]struct{}
		local   bool
	}
	newCandidate := func(m tsdb.BlockMeta, isLocal bool) candidate {
		c := candidate{meta: m, sources: make(map[ulid.ULID]struct{}), local: isLocal}
		c.sources[m.ULID] = struct{}{}
		if len(m.Compaction.Sources) > 0 {
			c.sources = make(map[ulid.ULID]struct{}, len(m.Compaction.Sources))
			for _, src := range m.Compaction.Sources {
				c.sources[src] = struct{}{}
			}
		}
		return c
	}
	covers := func(c, b candidate) bool {
		if c.meta.ULID == b.meta.ULID {
			return c.local
		}
		for src := range b.sources {
			if _, ok := c.sources[src]; !ok {
				return false
			}
		}
		// same sources, prefer the local block, then the newer block
		if len(c.sources) == len(b.sources) && !c.local {
			return c.meta.ULID.Compare(b.meta.ULID) > 0
		}
		return true
	}

	all := make([]candidate, 0, len(local)+len(s.metas))
	for _, m := range local {
		all = append(all, newCandidate(m, true))
	}
	for id, m := range s.metas {
		// a block that can't be read can't cover another block either
		if _, ok := s.failed[id]; ok {
			continue
		}
		all = append(all, newCandidate(m, false))
	}

	var selected []ulid.ULID
	for _, b := range all {
		if b.local || b.meta.MaxTime < mint || b.meta.MinTime > maxt {
			continue
		}
		covered := false
		for _, c := range all {
			if (c.local || c.meta.ULID != b.meta.ULID) && covers(c, b) {
				covered = true
				break
			}
		}
		if !covered {
			selected = append(selected, b.meta.ULID)
		}
	}
	return selected
}

// fetch downloads the blocks that are not cached yet and acquires the blocks. It waits for the
// downloads until the context is done, the downloads that are still running continue in the
// background.
func (s *remoteBlockStore) fetch(ctx context.Context, ids []ulid.ULID) []*cachedBlock {
	s.mu.Lock()
	for _, id := range ids {
		s.wanted[id]++
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		for _, id := range ids {
			if s.wanted[id]--; s.wanted[id] <= 0 {
				delete(s.wanted, id)
			}
		}
		s.mu.Unlock()
	}()

	var pending []<-chan singleflight.Result
	for _, id := range ids {
		if ch := s.startDownload(id); ch != nil {
			pending = append(pending, ch)
		}
	}
wait:
	for _, ch := range pending {
		select {
		case <-ch:
		case <-ctx.Done():
			break wait
		}
	}

	return s.acquire(ids)
}

// acquire returns the cached blocks of ids and takes a reference on each of them, so that they
// are not evicted until they are released. Blocks that are not cached yet are downloaded in
// the background.
func (s *remoteBlockStore) acquire(ids []ulid.ULID) []*cachedBlock {
	var acquired []*cachedBlock
	var missing []ulid.ULID

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	for _, id := range ids {
		b, ok := s.blocks[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		b.refs++
		s.lru.MoveToFront(b.elem)
		acquired = append(acquired, b)
	}
	s.mu.Unlock()

	for _, id := range missing {
		s.startDownload(id)
	}
	return acquired
}

func (s *remoteBlockStore) release(blocks []*cachedBlock) {
	if len(blocks) == 0 {
		return
	}

	var unused []*cachedBlock
	s.mu.Lock()
	for _, b := range blocks {
		b.refs--
		if b.dropped && b.refs == 0 && !s.closed {
			unused = append(unused, s.removeLocked(b))
		}
	}
	if !s.closed {
		unused = append(unused, s.evictLocked()...)
	}
	s.mu.Unlock()

	s.closeBlocks(unused)
}

// startDownload starts downloading the block if it is not cached, being downloaded or failed
// recently. It returns nil if there is nothing to wait for.
func (s *remoteBlockStore) startDownload(id ulid.ULID) <-chan singleflight.Result {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	if _, ok := s.blocks[id]; ok {
		return nil
	}
	if _, ok := s.metas[id]; !ok {
		return nil
	}
	if t, ok := s.failed[id]; ok && time.Since(t) < remoteBlockRetryInterval {
		return nil
	}

	return s.group.DoChan(id.String(), func() (any, error) {
		s.mu.Lock()
		// the previous download may have finished after the check above
		if _, ok := s.blocks[id]; ok || s.closed {
			s.mu.Unlock()
			return nil, nil
		}
		s.wg.Add(1)
		s.mu.Unlock()
		defer s.wg.Done()

		err := s.download(id)
		if err != nil {
			level.Warn(s.logger).Log("msg", "unable to download block from the file store", "block", id.String(), "err", err)
			s.mu.Lock()
			s.failed[id] = time.Now()
			s.mu.Unlock()
		}
		return nil, err
	})
}

func (s *remoteBlockStore) download(id ulid.ULID) error {
	s.downloads <- struct{}{}
	defer func() { <-s.downloads }()

	// download into a temporary directory first so that a partial block is never opened
	tmp, err := os.MkdirTemp(s.cacheDir, ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err = copyFromFileStore(tmp, filepath.Join(s.remoteDir, id.String()), s.backend); err != nil {
		return err
	}

	// every download gets its own directory, so that removing an evicted block can't
	// remove the files of a newer download of the same block
	dir := filepath.Join(s.cacheDir, fmt.Sprintf("%s-%d", id, s.seq.Add(1)))
	if err = os.Rename(filepath.Join(tmp, id.String()), dir); err != nil {
		return err
	}

	block, err := tsdb.OpenBlock(s.logger, dir, nil)
	if err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("could not open block: %w", err)
	}
	if block.Meta().ULID != id {
		block.Close()
		os.RemoveAll(dir)
		return fmt.Errorf("unexpected block id %s", block.Meta().ULID)
	}
	// a corrupt chunk would otherwise only show up when a query reads it and fail the query
	if err = verifyBlock(block); err != nil {
		block.Close()
		os.RemoveAll(dir)
		return fmt.Errorf("could not verify block: %w", err)
	}

	size, err := dirSize(dir)
	if err != nil {
		block.Close()
		os.RemoveAll(dir)
		return err
	}

	b := &cachedBlock{id: id, dir: dir, block: block, size: size}
	s.mu.Lock()
	_, inFileStore := s.metas[id]
	if s.closed || !inFileStore {
		s.mu.Unlock()
		s.closeBlocks([]*cachedBlock{b})
		return nil
	}
	b.elem = s.lru.PushFront(b)
	s.blocks[id] = b
	s.size += size
	evicted := s.evictLocked()
	s.mu.Unlock()

	s.closeBlocks(evicted)
	return nil
}

// evictLocked removes the least recently used blocks that are not in use until the cache fits
// in its size. The blocks in use or waited for are kept, so the cache can exceed its size while
// they are used.
func (s *remoteBlockStore) evictLocked() []*cachedBlock {
	var evicted []*cachedBlock
	for e := s.lru.Back(); e != nil && s.size > s.maxBytes; {
		prev := e.Prev()
		if b := e.Value.(*cachedBlock); b.refs == 0 && s.wanted[b.id] == 0 {
			evicted = append(evicted, s.removeLocked(b))
		}
		e = prev
	}
	return evicted
}

func (s *remoteBlockStore) removeLocked(b *cachedBlock) *cachedBlock {
	s.lru.Remove(b.elem)
	delete(s.blocks, b.id)
	s.size -= b.size
	return b
}

func (s *remoteBlockStore) closeBlocks(blocks []*cachedBlock) {
	for _, b := range blocks {
		if err := b.block.Close(); err != nil {
			level.Warn(s.logger).Log("msg", "unable to close block", "block", b.id.String(), "err", err)
		}
		if err := os.RemoveAll(b.dir); err != nil {
			level.Warn(s.logger).Log("msg", "unable to remove block from the cache", "block", b.id.String(), "err", err)
		}
	}
}

// close closes the cached blocks and removes the cache. The caller must make sure that no
// query is using the blocks. Downloads that are still running discard their blocks.
func (s *remoteBlockStore) close() {
	s.mu.Lock()
	s.closed = true
	blocks := make([]*cachedBlock, 0, len(s.blocks))
	for _, b := range s.blocks {
		blocks = append(blocks, b)
	}
	s.blocks = make(map[ulid.ULID]*cachedBlock)
	s.lru.Init()
	s.size = 0
	s.mu.Unlock()

	s.closeBlocks(blocks)
	if err := os.RemoveAll(s.cacheDir); err != nil {
		level.Warn(s.logger).Log("msg", "unable to remove the remote block cache", "err", err)
	}
}

// wait waits for the running downloads to finish.
func (s *remoteBlockStore) wait() {
	s.wg.Wait()
}

// remoteQuerier releases the file store blocks once the query is done with them.
type remoteQuerier struct {
	storage.Querier
	release func()
}

func (q *remoteQuerier) Close() error {
	err := q.Querier.Close()
	q.release()
	return err
}

// querier returns a querier that merges the local tsdb with the file store blocks that are
// not in the local tsdb. Samples with the same timestamp in more than one block are returned
// once, so overlapping blocks are not counted twice.
func (s *remoteBlockStore) querier(db *tsdb.DB, mint, maxt int64) (storage.Querier, error) {
	blocks := s.acquire(s.selectBlocks(mint, maxt, localBlockMetas(db)))

	q, err := db.Querier(mint, maxt)
	if err != nil {
		s.release(blocks)
		return nil, err
	}

	queriers := []storage.Querier{q}
	for _, b := range blocks {
		bq, bErr := tsdb.NewBlockQuerier(b.block, mint, maxt)
		if bErr != nil {
			level.Warn(s.logger).Log("msg", "unable to query block from the file store", "block", b.id.String(), "err", bErr)
			continue
		}
		queriers = append(queriers, bq)
	}

	return &remoteQuerier{
		Querier: storage.NewMergeQuerier(queriers, nil, storage.ChainedSeriesMerge),
		release: func() { s.release(blocks) },
	}, nil
}

func localBlockMetas(db *tsdb.DB) []tsdb.BlockMeta {
	blocks := db.Blocks()
	metas := make([]tsdb.BlockMeta, 0, len(blocks))
	for _, b := range blocks {
		metas = append(metas, b.Meta())
	}
	return metas
}

// verifyBlock reads every sample of the block and checks their count against the block meta.
func verifyBlock(block *tsdb.Block) error {
	ir, err := block.Index()
	if err != nil {
		return err
	}
	defer ir.Close()
	cr, err := block.Chunks()
	if err != nil {
		return err
	}
	defer cr.Close()

	name, value := index.AllPostingsKey()
	postings, err := ir.Postings(context.Background(), name, value)
	if err != nil {
		return err
	}

	var (
		builder labels.ScratchBuilder
		chks    []chunks.Meta
		it      chunkenc.Iterator
		samples uint64
	)
	for postings.Next() {
		if err = ir.Series(postings.At(), &builder, &chks); err != nil {
			return err
		}
		for _, meta := range chks {
			chk, chkErr := cr.Chunk(meta)
			if chkErr != nil {
				return chkErr
			}
			it = chk.Iterator(it)
			for it.Next() != chunkenc.ValNone {
				samples++
			}
			if it.Err() != nil {
				return it.Err()
			}
		}
	}
	if err = postings.Err(); err != nil {
		return err
	}
	if expected := block.Meta().Stats.NumSamples; samples != expected {
		return fmt.Errorf("block has %d samples, its meta lists %d", samples, expected)
	}
	return nil
}

func dirSize(dir string) (int64, error) {
	var size int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}
