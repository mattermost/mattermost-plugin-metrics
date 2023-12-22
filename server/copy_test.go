package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

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
