// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import "time"

const (
	PluginName     = "mattermost-plugin-metrics"
	tsdbDirName    = "data"
	metaFileName   = "meta.json"
	metaVersion1   = 1
	pluginDataDir  = "plugin-data"
	zipFileName    = "tsdb_dump.tar.gz"
	MaxRequestSize = 5 * 1024 * 1024 // 5MB
)

const (
	// scraperRetryInterval is how long a node waits before trying to become the scraping node again
	// after it failed to start scraping.
	scraperRetryInterval = time.Minute

	// KVStoreScraperNodeKey holds the cluster id of the node that is scraping the metrics.
	KVStoreScraperNodeKey = PluginName + "_scraper_node"
	// scraperNodeTTL is the expiry of KVStoreScraperNodeKey, the scraping node refreshes it before then.
	scraperNodeTTL = time.Minute

	// forwardedQueryTimeout bounds how long a node waits for the scraping node to answer a query.
	forwardedQueryTimeout = 60 * time.Second
	// MaxForwardedResponseSize caps the decompressed size of a forwarded query response.
	MaxForwardedResponseSize = 256 * 1024 * 1024 // 256MB
)
