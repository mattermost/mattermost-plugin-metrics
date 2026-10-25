// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"crypto/rand"
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

const (
	testMetric    = "test_metric"
	testRetention = 15 * 24 * time.Hour
)

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

// compactTestBlocks compacts the blocks into a new block in dir, its meta lists the blocks as
// its sources.
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
	return blockID(blockDir)
}

// corruptTestBlock flips the bytes of the chunks after the segment header.
func corruptTestBlock(t *testing.T, blockDir string) {
	t.Helper()
	segment := filepath.Join(blockDir, "chunks", "000001")
	data, err := os.ReadFile(segment)
	require.NoError(t, err)
	for i := 16; i < len(data)-4; i++ {
		data[i] ^= 0xff
	}
	require.NoError(t, os.WriteFile(segment, data, 0600)) //nolint:gosec // G703: test file in a temp dir
}

func blockID(blockDir string) ulid.ULID {
	return ulid.MustParse(filepath.Base(blockDir))
}

func newRestoreTestPlugin(backend filestore.FileBackend) *Plugin {
	return &Plugin{
		MattermostPlugin: plugin.MattermostPlugin{
			API: &fakeNodeAPI{cluster: newFakeCluster()},
		},
		fileBackend: backend,
		logger:      log.NewNopLogger(),
		closeChan:   make(chan bool),
	}
}

// localBlockIDs returns the blocks in the tsdb directory and fails on leftover temporary directories.
func localBlockIDs(t *testing.T, dir string) []ulid.ULID {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var ids []ulid.ULID
	for _, e := range entries {
		require.NotContains(t, e.Name(), tmpForCreationSuffix)
		if id, err := ulid.ParseStrict(e.Name()); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// openRestoredTSDB opens the tsdb in dir for the plugin, the way startScraping does.
func openRestoredTSDB(t *testing.T, p *Plugin, dir string) {
	t.Helper()
	opts := tsdb.DefaultOptions()
	opts.RetentionDuration = testRetention.Milliseconds()
	db, err := tsdb.Open(dir, nil, nil, opts, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	p.db = db
	p.queryEngine = p.newQueryEngine()
}

// countPerHour runs count_over_time over the hourly windows ending at start+1h, ..., end and
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

func testBase() int64 {
	return time.Now().Truncate(time.Hour).Add(-10 * 24 * time.Hour).UnixMilli()
}

func TestBlocksToRestore(t *testing.T) {
	base := testBase()
	hour := time.Hour.Milliseconds()
	newMeta := func(mint, maxt int64, sources ...ulid.ULID) tsdb.BlockMeta {
		id := ulid.MustNew(ulid.Now(), rand.Reader)
		time.Sleep(time.Millisecond) // keep the ids ordered
		if len(sources) == 0 {
			sources = []ulid.ULID{id}
		}
		return tsdb.BlockMeta{ULID: id, MinTime: mint, MaxTime: maxt, Compaction: tsdb.BlockMetaCompaction{Sources: sources}}
	}
	ids := func(metas []tsdb.BlockMeta) []ulid.ULID {
		var res []ulid.ULID
		for _, m := range metas {
			res = append(res, m.ULID)
		}
		return res
	}
	minT := base - hour

	first := newMeta(base, base+2*hour)
	second := newMeta(base+2*hour, base+4*hour)
	compacted := newMeta(base, base+4*hour, first.ULID, second.ULID)
	other := newMeta(base+hour, base+3*hour)

	t.Run("blocks that are local are skipped", func(t *testing.T) {
		require.Equal(t, []ulid.ULID{second.ULID}, ids(blocksToRestore([]tsdb.BlockMeta{first}, []tsdb.BlockMeta{first, second}, minT)))
	})

	t.Run("sources of a compacted block are skipped", func(t *testing.T) {
		remote := []tsdb.BlockMeta{first, second, compacted, other}
		require.Equal(t, []ulid.ULID{compacted.ULID, other.ULID}, ids(blocksToRestore(nil, remote, minT)))
	})

	t.Run("blocks covered by a local block are skipped", func(t *testing.T) {
		remote := []tsdb.BlockMeta{first, second, compacted, other}
		require.Equal(t, []ulid.ULID{other.ULID}, ids(blocksToRestore([]tsdb.BlockMeta{compacted}, remote, minT)))
	})

	t.Run("a local source does not cover the compacted block", func(t *testing.T) {
		remote := []tsdb.BlockMeta{second, compacted}
		require.Equal(t, []ulid.ULID{compacted.ULID}, ids(blocksToRestore([]tsdb.BlockMeta{first}, remote, minT)))
	})

	t.Run("blocks out of retention are skipped", func(t *testing.T) {
		require.Empty(t, blocksToRestore(nil, []tsdb.BlockMeta{first, second}, base+5*hour))
	})

	t.Run("the newer of the blocks with the same sources is restored", func(t *testing.T) {
		dup := newMeta(base, base+4*hour, first.ULID, second.ULID)
		require.Equal(t, []ulid.ULID{dup.ULID}, ids(blocksToRestore(nil, []tsdb.BlockMeta{compacted, dup}, minT)))
	})

	t.Run("newest blocks first", func(t *testing.T) {
		require.Equal(t, []ulid.ULID{second.ULID, other.ULID, first.ULID}, ids(blocksToRestore(nil, []tsdb.BlockMeta{first, other, second}, minT)))
	})
}

func TestRestoreFromRemote(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	// blocks of a previous scraping node, a compacted block is uploaded next to its sources
	dir := t.TempDir()
	old := createTestBlock(t, dir, "a", base, base+4*hour)
	first := createTestBlock(t, dir, "a", base+4*hour, base+6*hour)
	second := createTestBlock(t, dir, "a", base+6*hour, base+8*hour)
	compacted := compactTestBlocks(t, t.TempDir(), first, second)
	for _, b := range []string{old, first, second, compacted} {
		uploadTestBlock(t, backend, b)
	}

	// a local block, uploaded too, and a remote block with the same samples that overlaps it
	localDir := filepath.Join(t.TempDir(), "data")
	local := createTestBlock(t, localDir, "a", base+8*hour, base+10*hour)
	uploadTestBlock(t, backend, local)
	overlapping := uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "a", base+9*hour, base+11*hour))

	// a block of another node at the same time
	other := uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "b", base, base+2*hour))

	// a corrupt block, a block with only a meta, and a block out of retention
	corruptDir := createTestBlock(t, t.TempDir(), "c", base, base+2*hour)
	corruptTestBlock(t, corruptDir)
	uploadTestBlock(t, backend, corruptDir)
	missingDir := createTestBlock(t, t.TempDir(), "d", base, base+2*hour)
	require.NoError(t, copyFile(filepath.Join(missingDir, metaFileName), filepath.Join(remoteTestDir, blockID(missingDir).String(), metaFileName), backend.WriteFile))
	expiredMint := time.Now().Add(-testRetention - 3*time.Hour).UnixMilli()
	uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "e", expiredMint, expiredMint+2*hour))

	p := newRestoreTestPlugin(backend)
	require.NoError(t, p.restoreFromRemote(localDir, remoteTestDir, testRetention))

	expected := []ulid.ULID{blockID(old), blockID(compacted), blockID(local), overlapping, other}
	require.ElementsMatch(t, expected, localBlockIDs(t, localDir))

	// a second run has nothing to restore
	require.NoError(t, p.restoreFromRemote(localDir, remoteTestDir, testRetention))
	require.ElementsMatch(t, expected, localBlockIDs(t, localDir))

	openRestoredTSDB(t, p, localDir)
	require.Len(t, p.db.Blocks(), len(expected))

	// every hour has one sample per minute, the overlapping samples are not counted twice
	counts, resp := countPerHour(t, p, base, base+11*hour)
	require.Equal(t, hourlyCounts(11), counts["a"])
	require.Equal(t, hourlyCounts(2), counts["b"])
	require.Equal(t, (base+30_000)/1000, resp.DataStart)
}

func TestRestoreFromRemoteFallsBackToSources(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	hour := time.Hour.Milliseconds()

	dir := t.TempDir()
	first := createTestBlock(t, dir, "a", base, base+2*hour)
	second := createTestBlock(t, dir, "a", base+2*hour, base+4*hour)
	compacted := compactTestBlocks(t, t.TempDir(), first, second)
	corruptTestBlock(t, compacted)
	for _, b := range []string{first, second, compacted} {
		uploadTestBlock(t, backend, b)
	}

	p := newRestoreTestPlugin(backend)
	localDir := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.MkdirAll(localDir, 0750))
	require.NoError(t, p.restoreFromRemote(localDir, remoteTestDir, testRetention))

	ids := localBlockIDs(t, localDir)
	slices.SortFunc(ids, func(a, b ulid.ULID) int { return a.Compare(b) })
	require.Equal(t, []ulid.ULID{blockID(first), blockID(second)}, ids)

	openRestoredTSDB(t, p, localDir)
	counts, _ := countPerHour(t, p, base, base+4*hour)
	require.Equal(t, map[string][]float64{"a": hourlyCounts(4)}, counts)
}

func TestRestoreFromRemoteStopsOnDeactivate(t *testing.T) {
	backend := newTestFileBackend(t)
	base := testBase()
	uploadTestBlock(t, backend, createTestBlock(t, t.TempDir(), "a", base, base+time.Hour.Milliseconds()))

	p := newRestoreTestPlugin(backend)
	close(p.closeChan)
	localDir := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.MkdirAll(localDir, 0750))
	require.NoError(t, p.restoreFromRemote(localDir, remoteTestDir, testRetention))
	require.Empty(t, localBlockIDs(t, localDir))
}
