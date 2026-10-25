// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/v8/platform/shared/filestore"
)

const testMetric = "test_metric"

var remoteTestDir = filepath.Join(pluginDataDir, PluginName, tsdbDirName)

type testSample struct {
	t int64
	f float64
}

func (s testSample) T() int64                      { return s.t }
func (s testSample) F() float64                    { return s.f }
func (s testSample) H() *histogram.Histogram       { return nil }
func (s testSample) FH() *histogram.FloatHistogram { return nil }
func (s testSample) Type() chunkenc.ValueType      { return chunkenc.ValFloat }

func newTestFileBackend(t *testing.T) filestore.FileBackend {
	t.Helper()
	backend, err := filestore.NewFileBackend(filestore.FileBackendSettings{
		DriverName: "local",
		Directory:  t.TempDir(),
	})
	require.NoError(t, err)
	return backend
}

// createTestBlock writes a block to dir with one sample per minute in [mint, maxt) for the
// series of the given node and returns the block directory. The samples are at 30s past the
// minute, so that a 1h window ending on the hour holds 60 samples.
func createTestBlock(t *testing.T, dir, node string, mint, maxt int64) string {
	t.Helper()
	var samples []chunks.Sample
	for ts := mint + 30_000; ts < maxt; ts += time.Minute.Milliseconds() {
		samples = append(samples, testSample{t: ts, f: float64(ts)})
	}
	series := storage.NewListSeries(labels.FromStrings(labels.MetricName, testMetric, "node", node), samples)
	blockDir, err := tsdb.CreateBlock([]storage.Series{series}, dir, 30*24*time.Hour.Milliseconds(), log.NewNopLogger())
	require.NoError(t, err)
	return blockDir
}

// compactTestBlocks compacts the blocks into a new block in dir, its meta lists the
// blocks as its sources.
func compactTestBlocks(t *testing.T, dir string, blockDirs ...string) string {
	t.Helper()
	compactor, err := tsdb.NewLeveledCompactor(context.Background(), nil, log.NewNopLogger(), []int64{2 * time.Hour.Milliseconds()}, nil, nil)
	require.NoError(t, err)
	id, err := compactor.Compact(dir, blockDirs, nil)
	require.NoError(t, err)
	return filepath.Join(dir, id.String())
}

func uploadTestBlock(t *testing.T, backend filestore.FileBackend, blockDir string) ulid.ULID {
	t.Helper()
	require.NoError(t, copyDirectory(blockDir, filepath.Join(remoteTestDir, filepath.Base(blockDir)), backend.WriteFile))
	return ulid.MustParse(filepath.Base(blockDir))
}

func blockID(t *testing.T, blockDir string) ulid.ULID {
	t.Helper()
	return ulid.MustParse(filepath.Base(blockDir))
}

// newRemoteTestPlugin returns a plugin with a local tsdb in dbDir, which may already hold blocks,
// and the given file store.
func newRemoteTestPlugin(t *testing.T, dbDir string, backend filestore.FileBackend, maxBytes int64) *Plugin {
	t.Helper()

	db, err := tsdb.Open(dbDir, nil, nil, tsdb.DefaultOptions(), nil)
	require.NoError(t, err)

	store, err := newRemoteBlockStore(backend, remoteTestDir, remoteBlockCacheDir(dbDir), maxBytes, log.NewNopLogger())
	require.NoError(t, err)

	p := &Plugin{
		MattermostPlugin: plugin.MattermostPlugin{
			API: &fakeNodeAPI{cluster: newFakeCluster()},
		},
		db:           db,
		remoteBlocks: store,
		logger:       log.NewNopLogger(),
	}
	p.queryEngine = p.newQueryEngine()

	t.Cleanup(func() {
		store.wait()
		store.close()
		require.NoError(t, db.Close())
	})
	return p
}

func appendTestSamples(t *testing.T, db *tsdb.DB, node string, mint, maxt int64) {
	t.Helper()
	app := db.Appender(context.Background())
	lbls := labels.FromStrings(labels.MetricName, testMetric, "node", node)
	for ts := mint + 30_000; ts < maxt; ts += time.Minute.Milliseconds() {
		_, err := app.Append(0, lbls, ts, float64(ts))
		require.NoError(t, err)
	}
	require.NoError(t, app.Commit())
}

// countPerHour runs count_over_time over hourly windows ending at start+1h, ..., end and
// returns the counts per node.
func countPerHour(t *testing.T, p *Plugin, start, end int64) (map[string][]float64, BatchQueryResponse) {
	t.Helper()
	resp, err := p.runQueryBatch(context.Background(), BatchQueryRequest{
		Start:   time.UnixMilli(start).Add(time.Hour).Unix(),
		End:     time.UnixMilli(end).Unix(),
		Step:    3600,
		Queries: []string{"count_over_time(" + testMetric + "[1h])"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Empty(t, resp.Results[0].Error)

	counts := make(map[string][]float64)
	for _, s := range resp.Results[0].Series {
		for _, v := range s.Values {
			counts[s.Metric["node"]] = append(counts[s.Metric["node"]], v[1])
		}
	}
	return counts, resp
}

func hourlyCounts(hours int) []float64 {
	counts := make([]float64, hours)
	for i := range counts {
		counts[i] = 60
	}
	return counts
}

func cachedBlockIDs(s *remoteBlockStore) []ulid.ULID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []ulid.ULID
	for id := range s.blocks {
		ids = append(ids, id)
	}
	return ids
}

func sortedIDs(ids ...ulid.ULID) []ulid.ULID {
	ids = slices.Clone(ids)
	slices.SortFunc(ids, func(a, b ulid.ULID) int { return a.Compare(b) })
	return ids
}

func testBase() int64 {
	return time.Now().Truncate(time.Hour).Add(-10 * 24 * time.Hour).UnixMilli()
}

func TestRemoteBlocksOnlyInFileStore(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "a", base, base+6*hour))

	p := newRemoteTestPlugin(t, filepath.Join(t.TempDir(), "data"), backend, remoteBlockCacheMaxBytes)

	// the index has not been read yet
	counts, resp := countPerHour(t, p, base, base+6*hour)
	require.Empty(t, counts)
	require.Zero(t, resp.DataStart)

	require.NoError(t, p.remoteBlocks.refresh())

	counts, resp = countPerHour(t, p, base, base+6*hour)
	require.Equal(t, map[string][]float64{"a": hourlyCounts(6)}, counts)
	require.Equal(t, (base+30_000)/1000, resp.DataStart)
}

func TestRemoteBlocksSpanningLocalTSDB(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	// a block of a previous scraping node
	remoteOnly := createTestBlock(t, t.TempDir(), "a", base, base+4*hour)
	uploadTestBlock(t, backend, remoteOnly)

	// a local block that overlaps the previous one and was uploaded too
	dbDir := filepath.Join(t.TempDir(), "data")
	local := createTestBlock(t, dbDir, "a", base+2*hour, base+6*hour)
	uploadTestBlock(t, backend, local)

	p := newRemoteTestPlugin(t, dbDir, backend, remoteBlockCacheMaxBytes)
	appendTestSamples(t, p.db, "a", base+6*hour, base+9*hour)
	require.NoError(t, p.remoteBlocks.refresh())

	// the uploaded copy of the local block is not read
	require.Equal(t, []ulid.ULID{blockID(t, remoteOnly)}, p.remoteBlocks.selectBlocks(base, base+9*hour, localBlockMetas(p.db)))

	// every hour has exactly one sample per minute, overlapping samples are not counted twice
	counts, resp := countPerHour(t, p, base, base+9*hour)
	require.Equal(t, map[string][]float64{"a": hourlyCounts(9)}, counts)
	require.Equal(t, (base+30_000)/1000, resp.DataStart)

	// the head alone does not need the file store
	require.Empty(t, p.remoteBlocks.selectBlocks(base+7*hour, base+9*hour, localBlockMetas(p.db)))
}

func TestRemoteBlocksCompacted(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	dir := t.TempDir()
	first := createTestBlock(t, dir, "a", base, base+2*hour)
	second := createTestBlock(t, dir, "a", base+2*hour, base+4*hour)
	compacted := compactTestBlocks(t, t.TempDir(), first, second)
	// a block of another scraping node that overlaps in time
	other := createTestBlock(t, t.TempDir(), "b", base+hour, base+3*hour)
	for _, b := range []string{first, second, compacted, other} {
		uploadTestBlock(t, backend, b)
	}

	p := newRemoteTestPlugin(t, filepath.Join(t.TempDir(), "data"), backend, remoteBlockCacheMaxBytes)
	require.NoError(t, p.remoteBlocks.refresh())

	t.Run("sources of a compacted block are skipped", func(t *testing.T) {
		selected := p.remoteBlocks.selectBlocks(base, base+4*hour, nil)
		require.Equal(t, sortedIDs(blockID(t, compacted), blockID(t, other)), sortedIDs(selected...))

		counts, _ := countPerHour(t, p, base, base+4*hour)
		require.Equal(t, map[string][]float64{"a": hourlyCounts(4), "b": hourlyCounts(2)}, counts)
	})

	t.Run("blocks covered by a local block are skipped", func(t *testing.T) {
		local := []tsdb.BlockMeta{p.remoteBlocks.metas[blockID(t, compacted)]}
		require.Equal(t, []ulid.ULID{blockID(t, other)}, p.remoteBlocks.selectBlocks(base, base+4*hour, local))
	})

	t.Run("a local source does not cover the compacted block", func(t *testing.T) {
		local := []tsdb.BlockMeta{p.remoteBlocks.metas[blockID(t, first)]}
		selected := p.remoteBlocks.selectBlocks(base, base+4*hour, local)
		require.Equal(t, sortedIDs(blockID(t, compacted), blockID(t, other)), sortedIDs(selected...))
	})

	t.Run("blocks outside of the range are skipped", func(t *testing.T) {
		require.Empty(t, p.remoteBlocks.selectBlocks(base+5*hour, base+6*hour, nil))
	})

	t.Run("one of the blocks with the same sources is selected", func(t *testing.T) {
		meta := p.remoteBlocks.metas[blockID(t, compacted)]
		dup := meta
		dup.ULID = ulid.MustNew(ulid.Now(), nil)
		s := &remoteBlockStore{
			metas:  map[ulid.ULID]tsdb.BlockMeta{meta.ULID: meta, dup.ULID: dup},
			failed: map[ulid.ULID]time.Time{},
		}
		selected := s.selectBlocks(base, base+4*hour, nil)
		require.Len(t, selected, 1)

		// unless the block that covers the other one can't be read
		s.failed[selected[0]] = time.Now()
		require.Len(t, s.selectBlocks(base, base+4*hour, nil), 1)
		require.NotEqual(t, selected[0], s.selectBlocks(base, base+4*hour, nil)[0])
	})
}

func TestRemoteBlocksCorruptOrMissing(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	good := uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "a", base, base+2*hour))

	// a block with a corrupt chunk
	corruptDir := createTestBlock(t, t.TempDir(), "b", base, base+2*hour)
	segment := filepath.Join(corruptDir, "chunks", "000001")
	data, err := os.ReadFile(segment)
	require.NoError(t, err)
	for i := 16; i < len(data)-4; i++ {
		data[i] ^= 0xff
	}
	require.NoError(t, os.WriteFile(segment, data, 0600)) //nolint:gosec // G703: test file in a temp dir
	corrupt := uploadTestBlock(t, backend, corruptDir)

	// a block with only a meta in the file store
	missingDir := createTestBlock(t, t.TempDir(), "c", base, base+2*hour)
	missing := blockID(t, missingDir)
	require.NoError(t, copyFile(filepath.Join(missingDir, metaFileName), filepath.Join(remoteTestDir, missing.String(), metaFileName), backend.WriteFile))

	// a block that is removed after the index was read
	removed := uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "d", base, base+2*hour))

	p := newRemoteTestPlugin(t, filepath.Join(t.TempDir(), "data"), backend, remoteBlockCacheMaxBytes)
	require.NoError(t, p.remoteBlocks.refresh())
	require.NoError(t, backend.RemoveDirectory(filepath.Join(remoteTestDir, removed.String())))

	counts, _ := countPerHour(t, p, base, base+2*hour)
	require.Equal(t, map[string][]float64{"a": hourlyCounts(2)}, counts)

	require.Equal(t, []ulid.ULID{good}, cachedBlockIDs(p.remoteBlocks))
	p.remoteBlocks.mu.Lock()
	require.Contains(t, p.remoteBlocks.failed, corrupt)
	require.Contains(t, p.remoteBlocks.failed, missing)
	require.Contains(t, p.remoteBlocks.failed, removed)
	p.remoteBlocks.mu.Unlock()

	// failed blocks are not downloaded again right away
	require.Nil(t, p.remoteBlocks.startDownload(corrupt))

	// no partial downloads are left behind
	entries, err := os.ReadDir(p.remoteBlocks.cacheDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestRemoteBlocksEviction(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	dir := t.TempDir()
	first := createTestBlock(t, dir, "a", base, base+2*hour)
	second := createTestBlock(t, dir, "a", base+2*hour, base+4*hour)
	third := createTestBlock(t, dir, "a", base+4*hour, base+6*hour)
	for _, b := range []string{first, second, third} {
		uploadTestBlock(t, backend, b)
	}
	var size int64
	for _, b := range []string{first, second, third} {
		blockSize, err := dirSize(b)
		require.NoError(t, err)
		size = max(size, blockSize)
	}

	// the cache fits a single block
	p := newRemoteTestPlugin(t, filepath.Join(t.TempDir(), "data"), backend, size)
	s := p.remoteBlocks
	require.NoError(t, s.refresh())
	ctx := context.Background()

	pinnedFirst := s.fetch(ctx, []ulid.ULID{blockID(t, first)})
	require.Len(t, pinnedFirst, 1)
	pinnedSecond := s.fetch(ctx, []ulid.ULID{blockID(t, second)})
	require.Len(t, pinnedSecond, 1)

	// both blocks are in use, the cache exceeds its size until they are released
	require.ElementsMatch(t, []ulid.ULID{blockID(t, first), blockID(t, second)}, cachedBlockIDs(s))

	s.release(pinnedFirst)
	require.Equal(t, []ulid.ULID{blockID(t, second)}, cachedBlockIDs(s))
	_, err := os.Stat(pinnedFirst[0].dir)
	require.True(t, os.IsNotExist(err))

	s.release(pinnedSecond)
	require.Equal(t, []ulid.ULID{blockID(t, second)}, cachedBlockIDs(s))

	// the least recently used block is evicted
	s.release(s.fetch(ctx, []ulid.ULID{blockID(t, third)}))
	require.Equal(t, []ulid.ULID{blockID(t, third)}, cachedBlockIDs(s))

	// a block removed from the file store is dropped once it is no longer used
	pinnedThird := s.acquire([]ulid.ULID{blockID(t, third)})
	require.Len(t, pinnedThird, 1)
	require.NoError(t, backend.RemoveDirectory(filepath.Join(remoteTestDir, blockID(t, third).String())))
	require.NoError(t, s.refresh())
	require.Equal(t, []ulid.ULID{blockID(t, third)}, cachedBlockIDs(s))
	s.release(pinnedThird)
	require.Empty(t, cachedBlockIDs(s))
	_, err = os.Stat(pinnedThird[0].dir)
	require.True(t, os.IsNotExist(err))

	// queries still work with blocks that don't fit in the cache
	counts, _ := countPerHour(t, p, base, base+4*hour)
	require.Equal(t, map[string][]float64{"a": hourlyCounts(4)}, counts)
	require.Len(t, cachedBlockIDs(s), 1)
}

func TestRemoteBlocksClose(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()

	uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "a", base, base+time.Hour.Milliseconds()))

	cacheDir := filepath.Join(t.TempDir(), "cache")
	// leftovers of a previous run are removed
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "leftover"), 0750))

	s, err := newRemoteBlockStore(backend, remoteTestDir, cacheDir, remoteBlockCacheMaxBytes, log.NewNopLogger())
	require.NoError(t, err)
	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	require.Empty(t, entries)

	require.NoError(t, s.refresh())
	ids := s.selectBlocks(base, base+time.Hour.Milliseconds(), nil)
	require.Len(t, s.fetch(context.Background(), ids), 1)

	s.close()
	_, err = os.Stat(cacheDir)
	require.True(t, os.IsNotExist(err))
	require.Empty(t, s.acquire(ids))
}
