package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/prometheus/tsdb"
)

func (p *Plugin) createDump(ctx context.Context, min, max time.Time, remoteStorageDir string) (string, error) {
	// get the blocks if there is any block in the remote filestore
	blocks, err := p.fileBackend.ListDirectory(remoteStorageDir)
	if err != nil {
		return "", err
	} else if len(blocks) == 0 {
		return "", errors.New("no blocks in the remote sotrage")
	}

	zipFileNameRemote := filepath.Join(pluginDataDir, PluginName, zipFileName)
	dumpDir := filepath.Join(PluginName, "dump")
	for _, b := range blocks {
		// read block meta from the remote filestore and decide if they are older than the
		// retention period. If so, copy from file store.
		meta, rErr := readBlockMeta(filepath.Join(b, metaFileName), p.fileBackend.ReadFile)
		if rErr != nil {
			// we intentionally log with debug level here, file store returns wrapped errors
			// and to not pollute the logs, we simply reducing the log level here.
			p.API.LogDebug("unable to read meta file", "err", rErr)
			continue
		}

		metaMax := time.UnixMilli(meta.MaxTime)
		if metaMax.Before(max) && metaMax.After(min) {
			p.API.LogInfo("Fetching block from the filestore", "ulid", meta.ULID, "Max Time", max.String())

			err = copyFromFileStore(dumpDir, b, p.fileBackend)
			if err != nil {
				p.API.LogError("Error during fetching the block", "ulid", meta.ULID, "err", err)
			}
		}
	}

	db, err := tsdb.Open(dumpDir, p.logger, nil, &tsdb.Options{
		MinBlockDuration:           int64(2 * time.Hour / time.Millisecond),
		MaxBlockDuration:           int64(6 * time.Hour / time.Millisecond),
		AllowOverlappingCompaction: true,
	}, nil)
	if err != nil {
		return "", err
	}

	// we should compact the tsdb to remove/merge overlapping blocks. Also the older blocks
	// will be deleted but we didn't pull them in the first place anyway.
	err = db.Compact(ctx)
	if err != nil {
		return "", err
	}

	err = db.Close()
	if err != nil {
		return "", err
	}

	err = compressDirectory(dumpDir, zipFileName)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipFileName)

	err = os.RemoveAll(dumpDir)
	if err != nil {
		return "", err
	}

	f, err := os.Open(zipFileName)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = p.fileBackend.WriteFile(f, zipFileNameRemote)
	if err != nil {
		return "", err
	}

	return zipFileNameRemote, nil
}
