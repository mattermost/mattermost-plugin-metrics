package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattermost/mattermost/server/v8/platform/shared/filestore"
)

// copyDirectory recusively copies files within a directory
func copyDirectory(src, dest string, wr WriterFunc) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		sourcePath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		fileInfo, err := os.Stat(sourcePath)
		if err != nil {
			return err
		}

		i, err := entry.Info()
		if err != nil {
			return err
		}

		if i.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		switch fileInfo.Mode() & os.ModeType {
		case os.ModeDir:
			if err := copyDirectory(sourcePath, destPath, wr); err != nil {
				return err
			}
		default:
			if err := copyFile(sourcePath, destPath, wr); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(srcFile, dstFile string, wr WriterFunc) error {
	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}

	defer in.Close()

	_, err = wr(in, dstFile)
	if err != nil {
		return err
	}

	return nil
}

func copyFromFileStore(dst, src string, b filestore.FileBackend) error {
	if ok, err := b.FileExists(src); !ok {
		return nil
	} else if err != nil {
		return err
	}

	// create the dest parent if necessary
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}

	entries, listErr := b.ListDirectory(src)
	var pathError *fs.PathError
	if listErr != nil && errors.As(listErr, &pathError) {
		// means that this is a single file, copy the file to the local store
		reader, err := b.Reader(src)
		if err != nil {
			return err
		}

		trim := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
		fileDest := filepath.Join(dst, strings.TrimPrefix(src, trim))

		// create parent if there is no directory
		err = os.MkdirAll(filepath.Dir(fileDest), 0750)
		if err != nil {
			return err
		}
		f, err := os.Create(fileDest)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(f, reader)
		if err != nil {
			return err
		}
	} else if listErr != nil {
		return listErr
	}

	// it means this is a directory
	if len(entries) > 0 {
		trim := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
		fileDest := filepath.Join(dst, strings.TrimPrefix(src, trim))

		err := os.MkdirAll(fileDest, 0750)
		if err != nil {
			return err
		}
	}

	for _, entry := range entries {
		err := copyFromFileStore(dst, entry, b)
		if err != nil {
			return err
		}
	}

	return nil
}

func compressDirectory(sourceDir, compressedFile string) error {
	// Create a new archive file
	newZipFile, cErr := os.Create(compressedFile)
	if cErr != nil {
		return cErr
	}
	defer newZipFile.Close()

	zr := gzip.NewWriter(newZipFile)
	tw := tar.NewWriter(zr)

	// We will remove the srcDir from the file info later on
	srcDir := sourceDir

	// Walk through the directory and add files to the archive
	wErr := filepath.Walk(sourceDir, func(file string, fi os.FileInfo, _ error) error {
		if fi.Mode().IsDir() {
			return nil
		}

		newPath := file[len(srcDir):]

		header, err := tar.FileInfoHeader(fi, newPath)
		if err != nil {
			return err
		}

		header.Name = newPath
		if err = tw.WriteHeader(header); err != nil {
			return err
		}

		data, err := os.Open(file)
		if err != nil {
			return err
		}

		if _, err := io.Copy(tw, data); err != nil {
			return err
		}

		return nil
	})

	if wErr != nil {
		return wErr
	}

	if err := tw.Close(); err != nil {
		return err
	}

	if err := zr.Close(); err != nil {
		return err
	}

	return nil
}
