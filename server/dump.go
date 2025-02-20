// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/prometheus/tsdb"
)

type Dump struct {
	Path string
	MinT int64
	MaxT int64
}

func (p *Plugin) createDump(ctx context.Context, id string, min, max time.Time, remoteStorageDir string) (*Dump, error) {
	// get the blocks if there is any block in the remote filestore
	blocks, err := p.fileBackend.ListDirectory(remoteStorageDir)
	if err != nil {
		return nil, err
	} else if len(blocks) == 0 {
		return nil, errors.New("no blocks in the remote storage")
	}

	// we generate everything under a new directory to avoid conflicts
	// between simultaneous downloads
	dumpDir := filepath.Join("dump", id, "data")
	tempZipFile := filepath.Join(filepath.Dir(dumpDir), zipFileName)

	var actualMin, actualMax time.Time

	for _, b := range blocks {
		// read block meta from the remote filestore and decide if they are older than the
		// retention period. If they are within the retention period, copy the data
		// from the file store. Blocks older than the retention period are not copied.
		meta, rErr := readBlockMeta(filepath.Join(b, metaFileName), p.fileBackend.ReadFile)
		if rErr != nil {
			// we intentionally log with debug level here, file store returns wrapped errors
			// and to not pollute the logs, we simply reducing the log level here.
			p.API.LogDebug("unable to read meta file", "err", rErr)
			continue
		}

		metaMax := time.UnixMilli(meta.MaxTime)
		metaMin := time.UnixMilli(meta.MinTime)
		if metaMax.Before(max) && metaMax.After(min) {
			p.API.LogInfo("Fetching block from the filestore", "ulid", meta.ULID, "Max Time", max.String())

			err = copyFromFileStore(dumpDir, b, p.fileBackend)
			if err != nil {
				p.API.LogError("Error during fetching the block", "ulid", meta.ULID, "err", err)
			}

			if metaMax.After(actualMax) {
				actualMax = metaMax
			}

			if metaMin.Before(actualMin) || actualMin.IsZero() {
				actualMin = metaMin
			}
		}
	}

	// In order to save up space, we increase the maximum block duration to 6 hours (default is 2)
	// so that we can increase the compaction.
	db, err := tsdb.Open(dumpDir, p.logger, nil, &tsdb.Options{
		MinBlockDuration:           tsdb.DefaultBlockDuration,
		MaxBlockDuration:           3 * tsdb.DefaultBlockDuration,
		AllowOverlappingCompaction: true,
	}, nil)
	if err != nil {
		return nil, err
	}

	// we should compact the tsdb to remove/merge overlapping blocks. Also the older blocks
	// will be deleted but we didn't pull them in the first place anyway.
	err = db.Compact(ctx)
	if err != nil {
		return nil, err
	}

	err = db.Close()
	if err != nil {
		return nil, err
	}

	// Add plugin specific metadata
	customMetadata := map[string]any{
		"min": min.UnixMilli(),
		"max": max.UnixMilli(),
	}

	_, err = p.client.System.GeneratePacketMetadata(dumpDir, customMetadata)
	if err != nil {
		return nil, err
	}

	err = compressDirectory(dumpDir, tempZipFile)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempZipFile)

	err = os.RemoveAll(dumpDir)
	if err != nil {
		return nil, err
	}

	zipFileNameRemote := filepath.Join(pluginDataDir, PluginName, tempZipFile)
	err = copyFile(tempZipFile, zipFileNameRemote, p.fileBackend.WriteFile)
	if err != nil {
		return nil, err
	}

	return &Dump{
		Path: zipFileNameRemote,
		MinT: actualMin.UnixMilli(),
		MaxT: actualMax.UnixMilli(),
	}, nil
}
