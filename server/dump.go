package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/tsdb"
)

func (p *Plugin) createDump(max, min time.Time, remoteStorageDir string) (string, error) { //nolint:unused
	// get the blocks if there is any block in the remote filestore
	blocks, err := p.fileBackend.ListDirectory(remoteStorageDir)
	if err != nil {
		return "", err
	} else if len(blocks) == 0 {
		return "", errors.New("no blocks in the remote sotrage")
	}

	zipFileName := "tsdb_dump.zip"
	zipFileNameRemote := filepath.Join(pluginDataDir, PluginName, zipFileName)
	// read block meta from the remote filestore and decide if they are older than the
	// retention period. If so, delete.
	for _, b := range blocks {
		meta, err := readBlockMeta(filepath.Join(b, metaFileName), p.fileBackend.ReadFile)
		if err != nil {
			// we intentionally log with debug level here, file store returns wrapped errors
			// and to not pollute the logs, we simply reducing the log level here
			p.API.LogDebug("unable to read meta file", "err", err)
			continue
		}

		dumpDir := filepath.Join(PluginName, "dump")
		metaMax := time.UnixMilli(meta.MaxTime)
		if metaMax.Before(max) && metaMax.After(min) {
			p.API.LogInfo("Fetching block from the filestore", "ulid", meta.ULID, "Max Time", max.String())

			err = readFromFileStore(dumpDir, b, p.fileBackend)
			if err != nil {
				p.API.LogError("Error during fetching the block", "ulid", meta.ULID, "err", err)
			}
		}

		db, err := tsdb.Open(dumpDir, log.NewNopLogger(), nil, tsdb.DefaultOptions(), nil)
		if err != nil {
			return "", err
		}

		err = db.Compact()
		if err != nil {
			return "", err
		}

		err = db.Close()
		if err != nil {
			return "", err
		}

		err = zipDirectory(dumpDir, zipFileName)
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
	}

	return zipFileNameRemote, nil
}
