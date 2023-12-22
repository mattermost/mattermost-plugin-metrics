package main

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/stretchr/testify/require"
)

func TestReadBlockMeta(t *testing.T) {
	tuid := ulid.MustNew(ulid.Now(), rand.Reader)
	meta := tsdb.BlockMeta{
		MaxTime: time.Now().UnixMilli(),
		MinTime: time.Now().AddDate(0, 0, -1).UnixMilli(),
		ULID:    tuid,
		Version: metaVersion1,
		Compaction: tsdb.BlockMetaCompaction{
			Level:   1,
			Sources: []ulid.ULID{tuid},
		},
	}

	t.Run("read a valid meta", func(t *testing.T) {
		dir := t.TempDir()

		data, err := json.MarshalIndent(&meta, "", "  ")
		require.NoError(t, err)

		err = os.WriteFile(filepath.Join(dir, "meta.json"), data, 0600)
		require.NoError(t, err)

		newMeta, err := readBlockMeta(filepath.Join(dir, "meta.json"), os.ReadFile)
		require.NoError(t, err)
		require.Equal(t, meta, *newMeta)
	})

	t.Run("read an invalid meta version", func(t *testing.T) {
		dir := t.TempDir()

		meta.Version = 0
		defer func() { meta.Version = metaVersion1 }()

		data, err := json.MarshalIndent(&meta, "", "  ")
		require.NoError(t, err)

		err = os.WriteFile(filepath.Join(dir, "meta.json"), data, 0600)
		require.NoError(t, err)

		_, err = readBlockMeta(filepath.Join(dir, "meta.json"), os.ReadFile)
		require.Error(t, err)
	})

	t.Run("read non existent file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := readBlockMeta(filepath.Join(dir, "meta.json"), os.ReadFile)
		require.Error(t, err)
	})
}
