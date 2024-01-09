package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/tsdb"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
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

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	// the local tsdb to be used for head block
	db *tsdb.DB

	// filestore is being used long storage of the immutable blocks
	fileBackend filestore.FileBackend

	closeChan chan bool
	waitGroup sync.WaitGroup

	logger log.Logger
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	p.logger = &metricsLogger{api: p.API}

	appCfg := p.API.GetConfig()
	backend, err := filestore.NewFileBackend(filestore.NewFileBackendSettingsFromConfig(&appCfg.FileSettings, false, false))
	if err != nil {
		return fmt.Errorf("failed to initialize filebackend: %w", err)
	}
	p.fileBackend = backend

	if p.configuration == nil {
		p.configuration = new(configuration)
		p.configuration.SetDefaults()
	}

	if err = p.configuration.IsValid(); err != nil {
		return fmt.Errorf("could not validate config: %w", err)
	}

	// initiate local tsdb
	p.tsdbLock.Lock()
	defer p.tsdbLock.Unlock()
	p.db, err = tsdb.Open(*p.configuration.DBPath, p.logger, nil, &tsdb.Options{
		RetentionDuration:              int64(30 * 24 * time.Hour / time.Millisecond),
		AllowOverlappingCompaction:     *p.configuration.AllowOverlappingCompaction,
		EnableMemorySnapshotOnShutdown: *p.configuration.EnableMemorySnapshotOnShutdown,
	}, nil)
	if err != nil {
		return fmt.Errorf("could not open target tsdb: %w", err)
	}

	manager := scrape.NewManager(nil, p.logger, p.db)
	syncCh := make(chan map[string][]*targetgroup.Group)
	p.closeChan = make(chan bool)
	p.waitGroup = sync.WaitGroup{}

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
				SampleLimit:                uint(*p.configuration.SampleLimit),
				NativeHistogramBucketLimit: uint(*p.configuration.BucketLimit),
			},
		},
	}
	manager.ApplyConfig(scpCfg)

	// check if cluster is enabled
	if p.isHA() {
		p.waitGroup.Add(1)
		go func() {
			defer p.waitGroup.Done()
			ticker := time.NewTicker(time.Minute)
			defer func() {
				ticker.Stop()
			}()

			db, err := p.client.Store.GetMasterDB()
			if err != nil {
				p.API.LogError("Could not initiate the database connection", "error", err.Error())
				return
			}
			defer db.Close()

			for {
				select {
				case <-ticker.C:
					list, err := pingClusterDiscoveryTable(db, p.client.Store.DriverName(), *p.API.GetConfig().ClusterSettings.ClusterName)
					if err != nil {
						p.API.LogError("Could not ping the cluster discovery table", "error", err.Error())
						return
					}

					if !topologyChanged(nil, list) {
						continue
					}

					sync, err := generateTargetGroup(p.API.GetConfig(), list)
					if err != nil {
						p.API.LogError("Could not genarate target group for cluster", "error", err.Error())
						return
					}
					syncCh <- sync
				case <-p.closeChan:
					return
				}
			}
		}()
	} else {
		sync, err := generateTargetGroup(p.API.GetConfig(), nil)
		if err != nil {
			return fmt.Errorf("could not set scrape target :%w", err)
		}
		syncCh <- sync
	}

	// this goroutine will need to be re-structurd to listen a more channels
	// once we start supporting HA, we will need to listen the cluster change channel and
	// convert the []mmodel.ClusterDiscovery entries into map[string][]*targetgroup.Group
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
		p.syncFileStore()
	}()

	return nil
}

func (p *Plugin) OnDeactivate() error {
	p.tsdbLock.Lock()
	defer p.tsdbLock.Unlock()

	close(p.closeChan)
	p.waitGroup.Wait()

	p.API.LogInfo("Scrape manager stopped")

	if p.db != nil {
		return p.db.Close()
	}

	return nil
}
