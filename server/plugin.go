package main

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/v8/platform/shared/filestore"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/tsdb"
)

const PluginName = "mattermost-plugin-metrics"

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

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
}

// ServeHTTP demonstrates a plugin that handles HTTP requests by greeting the world.
func (p *Plugin) ServeHTTP(_ *plugin.Context, w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "Hello, world!")
}

func (p *Plugin) OnActivate() error {
	logger := &metricsLogger{api: p.API}

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

	// check if cluster is enabled
	if p.isHA() {
		// TODO(isacikgoz): get cluster info
		p.API.LogWarn("cluster meterics is not enabled")
	}

	// initiate local tsdb
	p.tsdbLock.Lock()
	defer p.tsdbLock.Unlock()
	p.db, err = tsdb.Open(*p.configuration.DBPath, logger, nil, &tsdb.Options{
		RetentionDuration:              int64(30 * 24 * time.Hour / time.Millisecond),
		AllowOverlappingCompaction:     *p.configuration.AllowOverlappingCompaction,
		EnableMemorySnapshotOnShutdown: *p.configuration.EnableMemorySnapshotOnShutdown,
	}, nil)
	if err != nil {
		return fmt.Errorf("could not open target tsdb: %w", err)
	}

	scrapeInterval := *p.configuration.ScrapeIntervalSeconds

	// TODO(isacikgoz): Use multiple targets for HA env
	lb := labels.NewBuilder(labels.FromMap(map[string]string{
		model.AddressLabel:        *appCfg.ServiceSettings.SiteURL,
		model.ScrapeIntervalLabel: fmt.Sprintf("%ds", scrapeInterval),
		model.ScrapeTimeoutLabel:  fmt.Sprintf("%ds", *p.configuration.ScrapeTimeoutSeconds),
	}))

	lset, origLabels, err := scrape.PopulateLabels(lb, &config.ScrapeConfig{
		JobName: "prometheus",
	}, true)
	if err != nil {
		return err
	}

	target := scrape.NewTarget(lset, origLabels, url.Values{})

	// Mutator is being used to apply labels to the metric samples
	sampleMutator = func(l labels.Labels) labels.Labels {
		return mutateSampleLabels(l, target, *p.configuration.HonorTimestamps, []*relabel.Config{})
	}

	return nil
}

func (p *Plugin) Deactivate() error {
	p.tsdbLock.Lock()
	defer p.tsdbLock.Unlock()

	if p.db != nil {
		return p.db.Close()
	}

	return nil
}
