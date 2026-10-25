// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import "time"

const (
	PluginName         = "mattermost-plugin-metrics"
	tsdbDirName        = "data"
	metaFileName       = "meta.json"
	metaVersion1       = 1
	pluginDataDir      = "plugin-data"
	zipFileName        = "tsdb_dump.tar.gz"
	MaxRequestSize     = 5 * 1024 * 1024 // 5MB
	localRetentionDays = 3 * 24 * time.Hour
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

const (
	// remoteBlockCacheMaxBytes bounds the disk space of the file store blocks downloaded for
	// the dashboard queries. The blocks used by a running query are not evicted.
	remoteBlockCacheMaxBytes = 2 * 1024 * 1024 * 1024 // 2GB
	// remoteBlockMaxDownloads limits the concurrent block downloads from the file store.
	remoteBlockMaxDownloads = 2
	// remoteBlockRetryInterval is how long a block that could not be downloaded is skipped.
	remoteBlockRetryInterval = 10 * time.Minute
	// remoteBlockFetchTimeout bounds how long a query batch waits for the file store blocks,
	// the downloads that take longer continue in the background.
	remoteBlockFetchTimeout = 20 * time.Second
	// remoteBlockFetchMargin is added before the start of a query batch when fetching the blocks
	// to cover the lookback and range selectors of the queries.
	remoteBlockFetchMargin = time.Hour
)
