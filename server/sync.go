// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/tsdb"
	"golang.org/x/exp/slices"
)

type WriterFunc func(io.Reader, string) (int64, error)
type ReaderFunc func(string) ([]byte, error)

// syncFileStore does two periodic jobs and listens closeChan to exit from its loop.
// 1. Synchronizes the local tsdb blocks with the remote filestore (mattermost-server)
// 2. Periodically deletes the obsolete blocks from the remote filestore
func (p *Plugin) syncFileStore() {
	localStorageDir := p.db.Dir()
	remoteStorageDir := filepath.Join(pluginDataDir, PluginName, tsdbDirName)

	tickFileStoreSync := time.NewTicker(time.Duration(*p.configuration.FileStoreSyncPeriodMinutes) * time.Minute)
	tickFileStoreCleanUp := time.NewTicker(time.Duration(*p.configuration.FileStoreCleanupPeriodMinutes) * time.Minute)
	defer func() {
		tickFileStoreSync.Stop()
		tickFileStoreCleanUp.Stop()

		// do the final sync with remote store
		err := p.syncWithRemote(localStorageDir, remoteStorageDir, *p.configuration.RetentionDurationDays)
		if err != nil {
			p.API.LogError("could not sync with remote store", "err", err)
		}
	}()

loop:
	for {
		select {
		case <-tickFileStoreSync.C:
			p.API.LogDebug("Syncing blocks with the filestore")
			err := p.syncWithRemote(localStorageDir, remoteStorageDir, *p.configuration.RetentionDurationDays)
			if err != nil {
				p.API.LogError("could not sync with remote store", "err", err)
				break loop
			}
		case <-tickFileStoreCleanUp.C:
			p.API.LogDebug("Cleaning up the filestore...")
			err := p.cleanupRemote(remoteStorageDir, *p.configuration.RetentionDurationDays)
			if err != nil {
				p.API.LogError("unable cleanup remote store, skipping cleanup", "err", err)
				continue
			}
		case <-p.closeChan:
			p.API.LogDebug("Filestore sync job stopped")
			return
		}
	}
}

func readBlockMeta(path string, reader ReaderFunc) (*tsdb.BlockMeta, error) {
	b, err := reader(path)
	if err != nil {
		return nil, err
	}

	var m tsdb.BlockMeta

	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	if m.Version != metaVersion1 {
		return nil, fmt.Errorf("unexpected meta version: %d", m.Version)
	}

	return &m, nil
}

func (p *Plugin) syncWithRemote(localStorageDir, remoteStorageDir string, retentionDays int) error {
	entries, err := os.ReadDir(localStorageDir)
	if err != nil {
		return fmt.Errorf("could not list directories in local storage: %w", err)
	}

	// set the deadline for retention
	ret := time.Now().AddDate(0, 0, -1*retentionDays)
	blocksToSync := make([]string, 0)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if _, parseErr := ulid.Parse(entry.Name()); parseErr != nil {
			// means that the directory is not a valid block
			continue
		}

		f := filepath.Join(localStorageDir, entry.Name(), metaFileName)
		meta, err2 := readBlockMeta(f, os.ReadFile)
		if err2 != nil && (os.IsNotExist(err2) || errors.Is(err2, fs.ErrNotExist)) {
			continue
		} else if err2 != nil {
			p.API.LogWarn("unable to read meta file", "err", err2)
			continue
		}

		max := time.UnixMilli(meta.MaxTime)
		if max.After(ret) {
			blocksToSync = append(blocksToSync, entry.Name())
		}
	}

	// we are using a pre-defined directory structure in the filestore
	// hence using the same remoteStorageDir variable here
	blocksInSFileStore := make(map[string]bool)
	dirs, err2 := p.fileBackend.ListDirectory(remoteStorageDir)
	if err2 != nil {
		p.API.LogError("could not list directory from filestore", "err", err2)
	}
	for i := range dirs {
		// we trim the parent dir from the dirs as filebackend returns the
		// absolute path (relative to filestore root)
		dir := filepath.Base(dirs[i])
		blocksInSFileStore[dir] = true
	}

	// we should skip the blocks those are already in the filestore
	// since the tsdb blocks are immutable, we don't need to worry whether
	// if the block is changed over time
	blocksToSync = slices.DeleteFunc(blocksToSync, func(s string) bool {
		_, ok := blocksInSFileStore[s]
		return ok
	})

	for _, block := range blocksToSync {
		err2 := copyDirectory(
			filepath.Join(localStorageDir, block),
			filepath.Join(remoteStorageDir, block),
			p.fileBackend.WriteFile,
		)
		if err2 != nil {
			p.API.LogError("could not write block to filestore", "err", err2)
		}
	}

	return nil
}

func (p *Plugin) cleanupRemote(remoteStorageDir string, retentionDays int) error {
	ret := time.Now().AddDate(0, 0, -1*retentionDays)

	// get the blocks if there is any block in the remote filestore
	blocks, err := p.fileBackend.ListDirectory(remoteStorageDir)
	if err != nil {
		return err
	}

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

		max := time.UnixMilli(meta.MaxTime)
		if max.Before(ret) {
			p.API.LogInfo("Deleting obsolete block from the filestore", "ulid", meta.ULID, "Max Time", max.String())
			err = p.fileBackend.RemoveDirectory(b)
			if err != nil {
				p.API.LogWarn("unable to remove block from filestore", "err", err)
			}
		}
	}
	return nil
}
