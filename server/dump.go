package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/tsdb"
	"gopkg.in/yaml.v3"

	root "github.com/mattermost/mattermost-plugin-metrics"
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

	db, err := tsdb.Open(dumpDir, log.NewNopLogger(), nil, tsdb.DefaultOptions(), nil)
	if err != nil {
		return "", err
	}

	// we should compact the tsdb to remove/merge overlapping blocks. Also the older blocks
	// will be deleted but we didn't pull them in the first place anyway.
	err = db.Compact(ctx)
	if err != nil {
		return "", err
	}

	err = db.CleanTombstones()
	if err != nil {
		return "", err
	}

	err = db.Close()
	if err != nil {
		return "", err
	}

	_, err = p.genarateMetadataForDump(filepath.Join(dumpDir, "metadata.yaml"), min, max)
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

	return zipFileNameRemote, nil
}

// Metadata is the auxiliary data added to a plugin downloadable.
// It should contain the required fields and may have it's custom values as well.
// TODO: move this to mattermost/server/public/pluginapi in the future
type metadata struct {
	ServerVersion string         `yaml:"server_version"`
	ServerID      string         `yaml:"server_id"`
	LicenseID     string         `yaml:"license_id"`
	PluginID      string         `yaml:"plugin_id"`
	Custom        map[string]any `yaml:"custom"`
}

func (p *Plugin) genarateMetadataForDump(path string, min, max time.Time) (*metadata, error) {
	m := &metadata{}
	if l := p.API.GetLicense(); l != nil {
		m.LicenseID = l.Id
	}

	m.ServerVersion = p.API.GetServerVersion()
	m.ServerID = p.API.GetTelemetryId()
	m.PluginID = root.Manifest.Id

	// Add plugin specific metadata
	customMetadata := map[string]any{
		"generated": time.Now().Format(time.RFC822),
		"min":       min.UnixMilli(),
		"max":       max.UnixMilli(),
	}

	m.Custom = customMetadata

	b, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	_, err = f.Write(b)
	if err != nil {
		return nil, err
	}

	return m, nil
}
