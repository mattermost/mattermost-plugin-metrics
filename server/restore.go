// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/index"
)

// tmpForCreationSuffix is the suffix the tsdb uses for blocks being created. The tsdb ignores
// these directories and removes the leftovers when it is opened.
const tmpForCreationSuffix = ".tmp-for-creation"

// restoreFromRemote copies the blocks in the filestore that are missing from the local tsdb into
// the local tsdb, e.g. the blocks of the node that was scraping before this one. The tsdb loads
// them on its next reload, which happens every minute. Blocks that can't be restored are
// logged and skipped.
func (p *Plugin) restoreFromRemote(localDir, remoteDir string, retention time.Duration) error {
	local, err := readLocalBlockMetas(localDir)
	if err != nil {
		return fmt.Errorf("could not read the local blocks: %w", err)
	}
	present := make(map[ulid.ULID]bool, len(local))
	for _, m := range local {
		present[m.ULID] = true
	}

	dirs, err := p.fileBackend.ListDirectory(remoteDir)
	if err != nil {
		return fmt.Errorf("could not list the blocks in the filestore: %w", err)
	}
	var remote []tsdb.BlockMeta
	for _, dir := range dirs {
		if p.isClosing() {
			return nil
		}
		id, parseErr := ulid.ParseStrict(filepath.Base(dir))
		if parseErr != nil || present[id] {
			continue
		}
		// the meta is uploaded after the index and chunks, so a block with a meta is complete
		m, metaErr := readBlockMeta(filepath.Join(dir, metaFileName), p.fileBackend.ReadFile)
		if metaErr != nil {
			p.API.LogDebug("unable to read meta file", "block", id.String(), "err", metaErr)
			continue
		}
		if m.ULID != id {
			p.API.LogWarn("block meta does not match its directory", "block", id.String())
			continue
		}
		remote = append(remote, *m)
	}

	minT := time.Now().Add(-retention).UnixMilli()
	for {
		todo := blocksToRestore(local, remote, minT)
		if len(todo) == 0 {
			return nil
		}

		for _, m := range todo {
			if p.isClosing() {
				return nil
			}

			if err := p.restoreBlock(localDir, remoteDir, m.ULID); err != nil {
				// the blocks this one covers are restored instead in the next round
				p.API.LogWarn("could not restore block from the filestore", "block", m.ULID.String(), "err", err)
				remote = slices.DeleteFunc(remote, func(r tsdb.BlockMeta) bool { return r.ULID == m.ULID })
				continue
			}
			p.API.LogInfo("Restored block from the filestore", "block", m.ULID.String(),
				"min_time", time.UnixMilli(m.MinTime).String(), "max_time", time.UnixMilli(m.MaxTime).String())
			local = append(local, m)
		}
	}
}

// isClosing reports whether the plugin is being deactivated.
func (p *Plugin) isClosing() bool {
	select {
	case <-p.closeChan:
		return true
	default:
		return false
	}
}

// blocksToRestore returns the remote blocks within retention that are not covered by a local
// block or by another remote block, newest first.
//
// The filestore holds the blocks of every node that was scraping, and compacted blocks are
// uploaded next to their sources. A block is covered by another block when all of its
// sources are part of the other block. Blocks of different nodes don't share sources, so
// they are all restored and the tsdb merges them where they overlap.
func blocksToRestore(local, remote []tsdb.BlockMeta, minT int64) []tsdb.BlockMeta {
	type candidate struct {
		meta    tsdb.BlockMeta
		sources map[ulid.ULID]struct{}
		local   bool
	}
	newCandidate := func(m tsdb.BlockMeta, isLocal bool) candidate {
		c := candidate{meta: m, sources: map[ulid.ULID]struct{}{m.ULID: {}}, local: isLocal}
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

	all := make([]candidate, 0, len(local)+len(remote))
	for _, m := range local {
		all = append(all, newCandidate(m, true))
	}
	for _, m := range remote {
		all = append(all, newCandidate(m, false))
	}

	var selected []tsdb.BlockMeta
	for _, b := range all {
		if b.local || b.meta.MaxTime < minT {
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
			selected = append(selected, b.meta)
		}
	}

	// the recent data is the most likely to be looked at
	slices.SortFunc(selected, func(a, b tsdb.BlockMeta) int {
		if c := cmp.Compare(b.MaxTime, a.MaxTime); c != 0 {
			return c
		}
		return b.ULID.Compare(a.ULID)
	})
	return selected
}

// restoreBlock downloads the block next to the tsdb blocks, verifies it and moves it into place.
// A corrupt block in the tsdb directory would fail every reload of the tsdb, so the block is
// only moved once it was read completely.
func (p *Plugin) restoreBlock(localDir, remoteDir string, id ulid.ULID) error {
	tmp := filepath.Join(localDir, id.String()+tmpForCreationSuffix)
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := copyFromFileStore(tmp, filepath.Join(remoteDir, id.String()), remoteDir, p.fileBackend); err != nil {
		return err
	}

	dir := filepath.Join(tmp, id.String())
	block, err := tsdb.OpenBlock(p.logger, dir, nil)
	if err != nil {
		return fmt.Errorf("could not open block: %w", err)
	}
	err = verifyBlock(block)
	if closeErr := block.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("could not verify block: %w", err)
	}
	if block.Meta().ULID != id {
		return fmt.Errorf("unexpected block id %s", block.Meta().ULID)
	}

	return os.Rename(dir, filepath.Join(localDir, id.String()))
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

// readLocalBlockMetas returns the metas of the blocks in the tsdb directory. A block whose meta
// can't be read is returned with only its id, so that it is not restored over.
func readLocalBlockMetas(dir string) ([]tsdb.BlockMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var metas []tsdb.BlockMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, parseErr := ulid.ParseStrict(entry.Name())
		if parseErr != nil {
			continue
		}
		m, metaErr := readBlockMeta(filepath.Join(dir, entry.Name(), metaFileName), os.ReadFile)
		if metaErr != nil {
			metas = append(metas, tsdb.BlockMeta{ULID: id})
			continue
		}
		metas = append(metas, *m)
	}
	return metas, nil
}
