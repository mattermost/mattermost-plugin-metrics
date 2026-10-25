// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-kit/log"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/tsdb"

	root "github.com/mattermost/mattermost-plugin-metrics"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/mattermost/mattermost/server/v8/platform/shared/filestore"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	client *pluginapi.Client

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// tsdbLock using mutual access to perform actions on tsdb.
	tsdbLock sync.RWMutex

	// singletonLock using mutually exclusive lock to run a single instance of the plugin
	singletonLock         *cluster.Mutex
	singletonLockAcquired bool

	// nodeID is the cluster id of this node, used to route forwarded queries back to it.
	nodeID string

	// pendingQueries holds the forwarded queries waiting for a response from the scraping node.
	pendingQueries     map[string]chan clusterQueryResponse
	pendingQueriesLock sync.Mutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	// the local tsdb to be used for head block
	db *tsdb.DB

	// queryEngine is the PromQL engine used for dashboard queries
	queryEngine *promql.Engine

	// remoteBlocks makes the blocks in the file store queryable, it is set along with db.
	remoteBlocks *remoteBlockStore

	// filestore is being used long storage of the immutable blocks
	fileBackend filestore.FileBackend

	closeChan chan bool
	waitGroup sync.WaitGroup

	logger log.Logger

	handler *handler

	scheduler *cluster.JobOnceScheduler
}

func (p *Plugin) OnActivate() error {
	if err := p.loadConfig(); err != nil {
		return fmt.Errorf("could not load plugin config: %w", err)
	}

	p.client = pluginapi.NewClient(p.API, p.Driver)
	p.logger = &metricsLogger{api: p.API}

	p.handler = newHandler(p)

	fileSettings := &p.API.GetUnsanitizedConfig().FileSettings
	fileSettings.SetDefaults(false) // some fields are nil, we should set those to default

	// since this is true by default, and it will be true if it's actually false.
	// see the discussion here: https://community.mattermost.com/core/pl/abd58h1majrx8y4xc9qrhtia9h
	if p.API.GetConfig().FileSettings.AmazonS3SSL == nil {
		fileSettings.AmazonS3SSL = mmModel.NewPointer(false)
	}

	fileBackendSettings := filestore.NewFileBackendSettingsFromConfig(fileSettings, false, true)
	backend, err := filestore.NewFileBackend(fileBackendSettings)
	if err != nil {
		return fmt.Errorf("failed to initialize filebackend: %w", err)
	}
	p.fileBackend = backend

	p.closeChan = make(chan bool)
	p.waitGroup = sync.WaitGroup{}
	p.pendingQueries = make(map[string]chan clusterQueryResponse)

	p.scheduler = cluster.GetJobOnceScheduler(p.API)
	p.scheduler.SetCallback(p.JobCallback)
	p.scheduler.Start()

	if !p.isHA() {
		return p.startScraping()
	}

	status, appErr := p.API.GetPluginStatus(root.Manifest.Id)
	if appErr != nil {
		return fmt.Errorf("could not get plugin status: %w", appErr)
	}
	p.nodeID = status.ClusterId

	// we are using a mutually exclusive lock to run a single instance of this plugin
	// we don't really need to collect metrics twice: although TSDB will take care
	// of overlapped blocks, it will increase the disk writes to the remote or local
	// disk.
	p.singletonLock, err = cluster.NewMutex(p.API, root.Manifest.Id)
	if err != nil {
		return err
	}

	p.waitGroup.Add(1)
	go func() {
		defer p.waitGroup.Done()
		p.runScraperElection(p.startScraping)
	}()

	return nil
}

// runScraperElection blocks until this node acquires the singleton lock and then calls start.
// Nodes keep waiting for the lock instead of giving up, so that another node takes over
// scraping when the scraping node leaves the cluster. It returns when the plugin is deactivated.
func (p *Plugin) runScraperElection(start func() error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-p.closeChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		p.API.LogDebug("Waiting for the lock to become the scraping node")
		// LockWithContext only fails when the context is canceled
		if err := p.singletonLock.LockWithContext(ctx); err != nil {
			return
		}
		p.singletonLockAcquired = true

		p.API.LogInfo("Acquired the lock, this node is collecting the metrics")
		err := start()
		if err == nil {
			return
		}

		// let another node try while we wait to retry
		p.API.LogError("Could not start scraping, releasing the lock", "err", err.Error())
		p.singletonLock.Unlock()
		p.singletonLockAcquired = false

		select {
		case <-time.After(scraperRetryInterval):
		case <-ctx.Done():
			return
		}
	}
}

// startScraping opens the local TSDB and starts scraping the metrics targets.
func (p *Plugin) startScraping() error {
	// The metrics plugin is dependent on the metrics endpoint being expoesed, so we need to ensure it is enabled.
	if cfg := p.API.GetUnsanitizedConfig(); cfg.MetricsSettings.Enable == nil || !*cfg.MetricsSettings.Enable {
		if lic := p.API.GetLicense(); lic != nil && *lic.Features.Metrics {
			p.API.LogInfo("Enabling metrics...")
			cfg.MetricsSettings.Enable = mmModel.NewPointer(true)
			if err2 := p.API.SaveConfig(cfg); err2 != nil {
				return fmt.Errorf("failed to save config: %w", err2)
			}
		}
	}

	var err error

	// initiate local tsdb
	p.tsdbLock.Lock()
	defer p.tsdbLock.Unlock()
	p.db, err = tsdb.Open(*p.configuration.DBPath, p.logger, nil, &tsdb.Options{
		RetentionDuration:              int64(localRetentionDays / time.Millisecond),
		AllowOverlappingCompaction:     *p.configuration.AllowOverlappingCompaction,
		EnableMemorySnapshotOnShutdown: *p.configuration.EnableMemorySnapshotOnShutdown,
	}, nil)
	if err != nil {
		return fmt.Errorf("could not open target tsdb: %w", err)
	}

	remoteBlocks, err := newRemoteBlockStore(
		p.fileBackend,
		filepath.Join(pluginDataDir, PluginName, tsdbDirName),
		remoteBlockCacheDir(*p.configuration.DBPath),
		remoteBlockCacheMaxBytes,
		p.logger,
	)
	if err != nil {
		p.db.Close()
		p.db = nil
		return err
	}
	p.remoteBlocks = remoteBlocks
	p.queryEngine = p.newQueryEngine()

	manager := scrape.NewManager(nil, p.logger, p.db)
	syncCh := make(chan map[string][]*targetgroup.Group)

	// we start the manager first, then apply the scrape config
	p.waitGroup.Add(1)
	go func() {
		defer p.waitGroup.Done()

		p.API.LogInfo("Running scrape manager...")
		err2 := manager.Run(syncCh)
		if err2 != nil {
			p.API.LogError("scrape manager exited unexpectedly", "err", err2)
		}
	}()

	scpCfg := &config.Config{
		ScrapeConfigs: []*config.ScrapeConfig{
			{
				JobName:                    "prometheus",
				Scheme:                     "http",
				MetricsPath:                "metrics",
				ScrapeInterval:             model.Duration(time.Duration(*p.configuration.ScrapeIntervalSeconds) * time.Second),
				ScrapeTimeout:              model.Duration(time.Duration(*p.configuration.ScrapeTimeoutSeconds) * time.Second),
				BodySizeLimit:              units.Base2Bytes(*p.configuration.BodySizeLimitBytes),
				HonorLabels:                *p.configuration.HonorTimestamps,
				SampleLimit:                p.configuration.SampleLimit,
				NativeHistogramBucketLimit: p.configuration.BucketLimit,
			},
		},
	}
	manager.ApplyConfig(scpCfg)

	// In HA, we want to continuously check for changes to the cluster (e.g. nodes joining/leaving).
	// In not-HA, we still want to regenerate targets in case plugins
	// providing metrics (e.g. Calls) started after we did.
	p.waitGroup.Add(1)
	go func() {
		defer p.waitGroup.Done()

		// a minute should be the smallest amount of time to ping the table
		// as the default scrape interval is already a minute.
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		var db *sqlx.DB
		if p.isHA() {
			idb, err := p.client.Store.GetMasterDB()
			if err != nil {
				p.API.LogError("Could not initiate the database connection", "error", err.Error())
				return
			}
			db = sqlx.NewDb(idb, p.client.Store.DriverName())
			defer db.Close()
		}

		var currentList []*mmModel.ClusterDiscovery
		for {
			select {
			case <-ticker.C:
				if p.isHA() {
					list, err := pingClusterDiscoveryTable(db, *p.API.GetConfig().ClusterSettings.ClusterName)
					if err != nil {
						p.API.LogError("Could not ping the cluster discovery table", "error", err.Error())
						return
					}

					if !topologyChanged(currentList, list) {
						continue
					}
					currentList = list
				}

				sync, err := p.generateTargetGroup(p.API.GetConfig(), currentList)
				if err != nil {
					p.API.LogError("Could not genarate target group for cluster", "error", err.Error())
					return
				}
				syncCh <- sync
			case <-p.closeChan:
				p.API.LogDebug("Cluster ping process stopped")
				return
			}
		}
	}()

	p.waitGroup.Add(1)
	go func() {
		defer p.waitGroup.Done()
		<-p.closeChan
		p.API.LogInfo("Stopping scrape manager...")
		manager.Stop()
	}()

	p.waitGroup.Add(1)
	go func() {
		defer p.waitGroup.Done()
		p.syncFileStore(remoteBlocks)
	}()

	if p.isHA() {
		// let the other nodes know where to forward the dashboard queries
		p.waitGroup.Add(1)
		go func() {
			defer p.waitGroup.Done()
			p.advertiseScraper()
		}()
	}

	return nil
}

func (p *Plugin) OnDeactivate() error {
	// stop the background jobs first, they may be waiting on the tsdb lock
	close(p.closeChan)
	p.waitGroup.Wait()

	p.API.LogInfo("Scrape manager stopped")

	p.tsdbLock.Lock()
	defer p.tsdbLock.Unlock()

	var err error
	if p.db != nil {
		err = p.db.Close()
		p.db = nil
		p.queryEngine = nil
	}
	if p.remoteBlocks != nil {
		p.remoteBlocks.close()
		p.remoteBlocks = nil
	}

	// the plugin mutex unlock panics if the lock was not acquired
	// so we need to check whether we actually acquired the lock
	if p.singletonLockAcquired {
		p.client.Store.Close()
		p.singletonLock.Unlock()
		p.singletonLockAcquired = false
	}

	return err
}

// ServeHTTP demonstrates a plugin that handles HTTP requests by greeting the world.
func (p *Plugin) ServeHTTP(_ *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.handler.ServeHTTP(w, r)
}
