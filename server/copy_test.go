package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestCompressDirectory(t *testing.T) {
	sampleFiles := []struct {
		Name, Content string
	}{
		{"file1.txt", "This is file 1."},
		{"subdir/file2.txt", "This is file 2."},
	}

	tempDir := filepath.Join(t.TempDir(), "compress")
	for _, file := range sampleFiles {
		filePath := filepath.Join(tempDir, file.Name)

		err := os.MkdirAll(filepath.Dir(filePath), 0755)
		require.NoError(t, err)

		err = os.WriteFile(filePath, []byte(file.Content), 0600)
		require.NoError(t, err)
	}

	zipFilePath := filepath.Join(t.TempDir(), "test.tar.gz")
	err := compressDirectory(tempDir, zipFilePath)
	require.NoError(t, err)

	_, err = os.Stat(zipFilePath)
	require.False(t, os.IsNotExist(err))

	zipFile, err := os.Open(zipFilePath)
	require.NoError(t, err)
	defer zipFile.Close()

	gzFile, err := gzip.NewReader(zipFile)
	require.NoError(t, err)
	defer gzFile.Close()

	tr := tar.NewReader(gzFile)

	expectedContents := map[string]string{
		"file1.txt":        "This is file 1.",
		"subdir/file2.txt": "This is file 2.",
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		require.NoError(t, err)

		// we only care about regular files in this test
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// the header has a trailing slash, gotta remove
		fileName := strings.TrimPrefix(header.Name, "/")
		content, ok := expectedContents[fileName]
		require.True(t, ok)
		expectedContent := []byte(content)

		zippedContent, err := io.ReadAll(tr)
		require.NoError(t, err)
		require.Equal(t, expectedContent, zippedContent)

		delete(expectedContents, fileName)
	}

	require.Empty(t, expectedContents)
}
