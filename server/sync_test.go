package main

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/stretchr/testify/require"
)

func localWriter(fr io.Reader, path string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return 0, err
	}

	fw, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return 0, err
	}
	defer fw.Close()

	written, err := io.Copy(fw, fr)
	if err != nil {
		return written, err
	}

	return written, nil
}

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

func TestCopyDirectory(t *testing.T) {
	dir := t.TempDir()
	dir2 := filepath.Join(dir, "sub")

	err := os.MkdirAll(dir2, 0750)
	require.NoError(t, err)

	fp1 := filepath.Join(dir, "file1")
	fp2 := filepath.Join(dir2, "file2")

	err = os.WriteFile(fp1, []byte("test"), 0600)
	require.NoError(t, err)

	err = os.WriteFile(fp2, []byte("test"), 0600)
	require.NoError(t, err)

	t.Run("move directory", func(t *testing.T) {
		newDir := t.TempDir()
		err := copyDirectory(dir, newDir, localWriter)
		require.NoError(t, err)

		b, err := os.ReadFile(filepath.Join(newDir, "file1"))
		require.NoError(t, err)
		require.Equal(t, "test", string(b))

		b, err = os.ReadFile(filepath.Join(newDir, "sub", "file2"))
		require.NoError(t, err)
		require.Equal(t, "test", string(b))
	})

	t.Run("don't move symlink", func(t *testing.T) {
		symlink := filepath.Join(dir, "symlink")
		err := os.Symlink(fp1, symlink)
		require.NoError(t, err)

		newDir := t.TempDir()
		err = copyDirectory(dir, newDir, localWriter)
		require.NoError(t, err)

		_, err = os.Open(filepath.Join(newDir, "file1"))
		require.Nil(t, err)

		_, err = os.Open(filepath.Join(newDir, "symlink"))
		require.True(t, os.IsNotExist(err))
	})
}

func TestCopy(t *testing.T) {
	t.Run("copy existing file to destination", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "afile")
		dst := filepath.Join(dir, "bfile")

		f, err := os.Create(src)
		require.NoError(t, err)

		_, err = f.WriteString("test")
		require.NoError(t, err)

		err = f.Sync()
		require.NoError(t, err)

		err = copyFile(src, dst, localWriter)
		require.NoError(t, err)

		b, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.Equal(t, "test", string(b))
	})

	t.Run("copy non-existing file", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "afile")
		dst := filepath.Join(dir, "bfile")

		err := copyFile(src, dst, localWriter)
		require.True(t, os.IsNotExist(err))
	})
}
