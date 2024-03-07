package main

const (
	PluginName         = "mattermost-plugin-metrics"
	tsdbDirName        = "data"
	metaFileName       = "meta.json"
	metaVersion1       = 1
	pluginDataDir      = "plugin-data"
	zipFileName        = "tsdb_dump.tar.gz"
	MaxRequestSize     = 5 * 1024 * 1024 // 5MB
	localRetentionDays = 3
)
