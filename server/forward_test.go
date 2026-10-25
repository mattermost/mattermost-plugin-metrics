// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
)

// fakeCluster simulates the parts of a Mattermost cluster the plugin relies on:
// a shared KV store and plugin cluster events between nodes.
type fakeCluster struct {
	mu    sync.Mutex
	kv    map[string][]byte
	nodes map[string]*Plugin
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{
		kv:    make(map[string][]byte),
		nodes: make(map[string]*Plugin),
	}
}

func (c *fakeCluster) addNode(t *testing.T, nodeID string) *Plugin {
	t.Helper()

	p := &Plugin{
		MattermostPlugin: plugin.MattermostPlugin{
			API: &fakeNodeAPI{cluster: c},
		},
		nodeID:         nodeID,
		closeChan:      make(chan bool),
		pendingQueries: make(map[string]chan clusterQueryResponse),
	}
	lock, err := cluster.NewMutex(p.API, "metrics")
	require.NoError(t, err)
	p.singletonLock = lock

	c.mu.Lock()
	c.nodes[nodeID] = p
	c.mu.Unlock()

	return p
}

func (c *fakeCluster) get(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.kv[key]
}

type fakeNodeAPI struct {
	plugin.API
	cluster *fakeCluster
}

func (a *fakeNodeAPI) GetConfig() *model.Config {
	cfg := &model.Config{}
	cfg.SetDefaults()
	cfg.ClusterSettings.Enable = model.NewPointer(true)
	return cfg
}

func (a *fakeNodeAPI) LogDebug(string, ...any) {}
func (a *fakeNodeAPI) LogInfo(string, ...any)  {}
func (a *fakeNodeAPI) LogWarn(string, ...any)  {}
func (a *fakeNodeAPI) LogError(string, ...any) {}

func (a *fakeNodeAPI) KVGet(key string) ([]byte, *model.AppError) {
	return a.cluster.get(key), nil
}

func (a *fakeNodeAPI) KVSetWithOptions(key string, value []byte, opts model.PluginKVSetOptions) (bool, *model.AppError) {
	a.cluster.mu.Lock()
	defer a.cluster.mu.Unlock()

	if opts.Atomic && !bytes.Equal(a.cluster.kv[key], opts.OldValue) {
		return false, nil
	}
	if value == nil {
		delete(a.cluster.kv, key)
	} else {
		a.cluster.kv[key] = value
	}
	return true, nil
}

func (a *fakeNodeAPI) KVCompareAndDelete(key string, oldValue []byte) (bool, *model.AppError) {
	a.cluster.mu.Lock()
	defer a.cluster.mu.Unlock()

	if !bytes.Equal(a.cluster.kv[key], oldValue) {
		return false, nil
	}
	delete(a.cluster.kv, key)
	return true, nil
}

func (a *fakeNodeAPI) PublishPluginClusterEvent(ev model.PluginClusterEvent, opts model.PluginClusterEventSendOptions) error {
	a.cluster.mu.Lock()
	target, ok := a.cluster.nodes[opts.TargetId]
	a.cluster.mu.Unlock()
	if !ok {
		return fmt.Errorf("node %q not found", opts.TargetId)
	}

	target.OnPluginClusterEvent(&plugin.Context{}, ev)
	return nil
}

func openTestTSDB(t *testing.T, p *Plugin) {
	t.Helper()

	db, err := tsdb.Open(t.TempDir(), log.NewNopLogger(), nil, tsdb.DefaultOptions(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	app := db.Appender(context.Background())
	now := time.Now()
	for i := 10; i >= 0; i-- {
		ts := now.Add(-time.Duration(i) * time.Minute).UnixMilli()
		_, err = app.Append(0, labels.FromStrings("__name__", "test_metric", "instance", "node-a"), ts, 42)
		require.NoError(t, err)
	}
	require.NoError(t, app.Commit())

	p.logger = log.NewNopLogger()
	p.db = db
	p.queryEngine = p.newQueryEngine()
}

func testBatchQueryRequest() BatchQueryRequest {
	now := time.Now().Unix()
	return BatchQueryRequest{
		Start:   now - 5*60,
		End:     now,
		Step:    60,
		Queries: []string{"test_metric"},
	}
}

func TestQueryBatchForwarding(t *testing.T) {
	t.Run("non-scraping node gets the results from the scraping node", func(t *testing.T) {
		c := newFakeCluster()
		scraper := c.addNode(t, "node-a")
		other := c.addNode(t, "node-b")
		openTestTSDB(t, scraper)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			scraper.advertiseScraper()
		}()
		require.Eventually(t, func() bool {
			return string(c.get(KVStoreScraperNodeKey)) == "node-a"
		}, time.Second, 10*time.Millisecond)

		resp, err := other.QueryBatch(context.Background(), testBatchQueryRequest())
		require.NoError(t, err)
		require.Len(t, resp.Results, 1)
		require.Empty(t, resp.Results[0].Error)
		require.Len(t, resp.Results[0].Series, 1)
		require.Equal(t, "node-a", resp.Results[0].Series[0].Metric["instance"])
		require.NotEmpty(t, resp.Results[0].Series[0].Values)
		require.Equal(t, float64(42), resp.Results[0].Series[0].Values[0][1])
		require.NotZero(t, resp.DataStart)
		require.Equal(t, 60, resp.ScrapeIntervalSeconds)
		require.Empty(t, other.pendingQueries)

		// the scraping node stops advertising itself on deactivation
		close(scraper.closeChan)
		wg.Wait()
		require.Nil(t, c.get(KVStoreScraperNodeKey))
	})

	t.Run("scraping node answers locally", func(t *testing.T) {
		c := newFakeCluster()
		scraper := c.addNode(t, "node-a")
		openTestTSDB(t, scraper)

		resp, err := scraper.QueryBatch(context.Background(), testBatchQueryRequest())
		require.NoError(t, err)
		require.Len(t, resp.Results[0].Series, 1)
	})

	t.Run("no scraping node", func(t *testing.T) {
		c := newFakeCluster()
		other := c.addNode(t, "node-b")

		_, err := other.QueryBatch(context.Background(), testBatchQueryRequest())
		require.ErrorIs(t, err, errScraperUnavailable)
	})

	t.Run("scraping node left the cluster", func(t *testing.T) {
		c := newFakeCluster()
		other := c.addNode(t, "node-b")
		c.kv[KVStoreScraperNodeKey] = []byte("node-gone")

		_, err := other.QueryBatch(context.Background(), testBatchQueryRequest())
		require.ErrorIs(t, err, errScraperUnavailable)
	})

	t.Run("advertised node is not scraping anymore", func(t *testing.T) {
		c := newFakeCluster()
		c.addNode(t, "node-a")
		other := c.addNode(t, "node-b")
		c.kv[KVStoreScraperNodeKey] = []byte("node-a")

		_, err := other.QueryBatch(context.Background(), testBatchQueryRequest())
		require.ErrorIs(t, err, errScraperUnavailable)
		require.ErrorContains(t, err, "no longer collecting")
	})

	t.Run("does not overwrite another node's entry on deactivation", func(t *testing.T) {
		c := newFakeCluster()
		scraper := c.addNode(t, "node-a")

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			scraper.advertiseScraper()
		}()
		require.Eventually(t, func() bool {
			return string(c.get(KVStoreScraperNodeKey)) == "node-a"
		}, time.Second, 10*time.Millisecond)

		c.mu.Lock()
		c.kv[KVStoreScraperNodeKey] = []byte("node-b")
		c.mu.Unlock()

		close(scraper.closeChan)
		wg.Wait()
		require.Equal(t, "node-b", string(c.get(KVStoreScraperNodeKey)))
	})
}

func TestRunScraperElection(t *testing.T) {
	type node struct {
		p       *Plugin
		started chan struct{}
		done    chan struct{}
	}
	startNode := func(p *Plugin, start func() error) *node {
		n := &node{p: p, started: make(chan struct{}, 1), done: make(chan struct{})}
		go func() {
			defer close(n.done)
			p.runScraperElection(func() error {
				if err := start(); err != nil {
					return err
				}
				n.started <- struct{}{}
				return nil
			})
		}()
		return n
	}
	ok := func() error { return nil }
	waitFor := func(t *testing.T, ch <-chan struct{}, msg string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatal(msg)
		}
	}

	t.Run("another node takes over when the scraping node leaves", func(t *testing.T) {
		c := newFakeCluster()
		a := startNode(c.addNode(t, "node-a"), ok)
		waitFor(t, a.started, "node-a did not start scraping")
		waitFor(t, a.done, "election did not return after starting")
		require.True(t, a.p.singletonLockAcquired)

		b := startNode(c.addNode(t, "node-b"), ok)
		select {
		case <-b.started:
			t.Fatal("node-b started scraping while node-a holds the lock")
		case <-time.After(2 * time.Second):
		}

		// node-a is deactivated and releases the lock
		a.p.singletonLock.Unlock()

		waitFor(t, b.started, "node-b did not take over")
		require.True(t, b.p.singletonLockAcquired)
		b.p.singletonLock.Unlock()
	})

	t.Run("releases the lock when scraping cannot start", func(t *testing.T) {
		c := newFakeCluster()
		attempted := make(chan struct{}, 1)
		a := startNode(c.addNode(t, "node-a"), func() error {
			attempted <- struct{}{}
			return errors.New("disk full")
		})
		waitFor(t, attempted, "node-a did not try to start scraping")

		b := startNode(c.addNode(t, "node-b"), ok)
		waitFor(t, b.started, "node-b did not take over")

		close(a.p.closeChan)
		waitFor(t, a.done, "election did not return on deactivation")
		require.False(t, a.p.singletonLockAcquired)
		b.p.singletonLock.Unlock()
	})

	t.Run("stops waiting on deactivation", func(t *testing.T) {
		c := newFakeCluster()
		c.kv["mutex_metrics"] = []byte{1} // held by another node
		b := startNode(c.addNode(t, "node-b"), ok)

		close(b.p.closeChan)
		waitFor(t, b.done, "election did not return on deactivation")
		require.False(t, b.p.singletonLockAcquired)
	})
}
