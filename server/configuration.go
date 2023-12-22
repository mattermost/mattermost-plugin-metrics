package main

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"reflect"

	"github.com/alecthomas/units"
	"github.com/pkg/errors"
	promModel "github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/mattermost/mattermost/server/public/model"
)

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration. Any public fields will be
// deserialized from the Mattermost server configuration in OnConfigurationChange.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// configuration can change at any time, access to the configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the configuration, and clone the entire
// struct whenever it changes. You may replace this with whatever strategy you choose.
//
// If you add non-reference types to your configuration struct, be sure to rewrite Clone as a deep
// copy appropriate for your types.
type configuration struct {
	DBPath                         *string
	AllowOverlappingCompaction     *bool
	EnableMemorySnapshotOnShutdown *bool
	BodySizeLimitBytes             *int64
	// More than this many samples post metric-relabeling will cause the scrape to fail. 0 means no limit.
	SampleLimit *int
	// More than this many buckets in a native histogram will cause the scrape to fail.
	BucketLimit *int
	// Indicator whether the scraped timestamps should be respected.
	HonorTimestamps *bool
	// Option to enable the experimental in-memory metadata storage and append metadata to the WAL.
	EnableMetadataStorage *bool
	// Scrape interval is the time between polling the /metrics endpoint
	ScrapeIntervalSeconds *int
	// Screpe timeout tells scraper to give up on the poll for a single scrape attempt
	ScrapeTimeoutSeconds *int
	// RetentionDurationDays defines the retention time for the tsdb blocks
	RetentionDurationDays *int
	// FileStoreSyncPeriodMinutes is the period to sync local store with the remote filestore
	FileStoreSyncPeriodMinutes *int
	// FileStoreCleanupPeriodMinutes is the period to run cleanup job in the filestore
	FileStoreCleanupPeriodMinutes *int
}

func (c *configuration) SetDefaults() {
	if c.DBPath == nil {
		c.DBPath = model.NewString(filepath.Join(PluginName, tsdbDirName))
	}
	if c.AllowOverlappingCompaction == nil {
		c.AllowOverlappingCompaction = model.NewBool(true)
	}
	if c.EnableMemorySnapshotOnShutdown == nil {
		c.EnableMemorySnapshotOnShutdown = model.NewBool(true)
	}
	if c.BodySizeLimitBytes == nil {
		c.BodySizeLimitBytes = model.NewInt64(int64(units.GiB))
	}
	if c.SampleLimit == nil {
		c.SampleLimit = model.NewInt(0)
	}
	if c.BucketLimit == nil {
		c.BucketLimit = model.NewInt(0)
	}
	if c.HonorTimestamps == nil {
		c.HonorTimestamps = model.NewBool(true)
	}
	if c.EnableMetadataStorage == nil {
		c.EnableMetadataStorage = model.NewBool(true)
	}
	if c.ScrapeIntervalSeconds == nil {
		c.ScrapeIntervalSeconds = model.NewInt(60)
	}
	if c.ScrapeTimeoutSeconds == nil {
		c.ScrapeTimeoutSeconds = model.NewInt(10)
	}
	if c.RetentionDurationDays == nil {
		c.RetentionDurationDays = model.NewInt(15)
	}
	if c.FileStoreSyncPeriodMinutes == nil {
		c.FileStoreSyncPeriodMinutes = model.NewInt(60)
	}
	if c.FileStoreCleanupPeriodMinutes == nil {
		c.FileStoreCleanupPeriodMinutes = model.NewInt(120)
	}
}

func (c *configuration) IsValid() error {
	if *c.ScrapeIntervalSeconds < 1 {
		return errors.New("scrape interval should be greater than zero")
	}
	if *c.ScrapeTimeoutSeconds < 1 {
		return errors.New("scrape timeout should be greater than zero")
	}
	if *c.BodySizeLimitBytes < 100 {
		return errors.New("openmetrics body size is not realistic, should be greater than 100 bytes")
	}
	return nil
}

// Clone deep copies the configuration.
func (c *configuration) Clone() (*configuration, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	clone := configuration{}
	if err = json.Unmarshal(b, &clone); err != nil {
		return nil, err
	}

	return &clone, nil
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *Plugin) setConfiguration(configuration *configuration) error {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return nil
		}

		return errors.New("setConfiguration called with the existing configuration")
	}

	if err := configuration.IsValid(); err != nil {
		return fmt.Errorf("setConfiguration: configuration is not valid: %w", err)
	}

	p.configuration = configuration

	return nil
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	serverConfig := p.API.GetConfig()
	if serverConfig == nil {
		p.API.LogError("OnConfigurationChange: failed to get server config")
	}

	if err := p.loadConfig(); err != nil {
		return fmt.Errorf("OnConfigurationChange: failed to load config: %w", err)
	}

	return nil
}

func (p *Plugin) loadConfig() error {
	cfg := new(configuration)

	// Load the public configuration fields from the Mattermost server configuration.
	if err := p.API.LoadPluginConfiguration(cfg); err != nil {
		return fmt.Errorf("loadConfig: failed to load plugin configuration: %w", err)
	}

	// Set defaults in case anything is missing.
	cfg.SetDefaults()

	return p.setConfiguration(cfg)
}

func (p *Plugin) ConfigurationWillBeSaved(newCfg *model.Config) (*model.Config, error) {
	if newCfg == nil {
		p.API.LogWarn("newCfg should not be nil")
		return nil, nil
	}

	configData := newCfg.PluginSettings.Plugins[PluginName]

	js, err := json.Marshal(configData)
	if err != nil {
		p.API.LogError("failed to marshal config data", "error", err.Error())
		return nil, nil
	}

	var cfg configuration
	if err := json.Unmarshal(js, &cfg); err != nil {
		p.API.LogError("failed to unmarshal config data", "error", err.Error())
		return nil, nil
	}

	// Setting defaults prevents errors in case the plugin is updated after a new
	// setting has been added. In this case the default value will be used.
	cfg.SetDefaults()

	if err := cfg.IsValid(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Plugin) isHA() bool {
	cfg := p.API.GetConfig()

	if cfg == nil {
		return false
	}

	return cfg.ClusterSettings.Enable != nil && *cfg.ClusterSettings.Enable
}

func generateTargetGroup(appCfg *model.Config, nodes []*model.ClusterDiscovery) (map[string][]*targetgroup.Group, error) {
	host, port, err := net.SplitHostPort(*appCfg.MetricsSettings.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("could not parse the listen address %q", *appCfg.MetricsSettings.ListenAddress)
	}

	sync := make(map[string][]*targetgroup.Group)
	if nodes == nil || len(nodes) < 2 {
		if host == "" {
			host = "localhost"
		}

		sync["prometheus"] = []*targetgroup.Group{
			{
				Targets: []promModel.LabelSet{
					{
						promModel.AddressLabel: promModel.LabelValue(net.JoinHostPort(host, port)),
					},
				},
			},
		}

		return sync, nil
	}

	targets := make([]promModel.LabelSet, len(nodes))
	for i, node := range nodes {
		targets[i] = promModel.LabelSet{
			promModel.AddressLabel: promModel.LabelValue(net.JoinHostPort(node.Hostname, port)),
		}
	}

	sync["prometheus"] = []*targetgroup.Group{
		{
			Targets: targets,
		},
	}

	return sync, nil
}
