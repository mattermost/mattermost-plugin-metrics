package main

const (
	scrapeAcceptHeader = `application/openmetrics-text;version=1.0.0,application/openmetrics-text;version=0.0.1;q=0.75,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`
	PluginName         = "mattermost-plugin-metrics"
	ScraperVersion     = "1.0"
	UserAgent          = PluginName + "/" + ScraperVersion
)
