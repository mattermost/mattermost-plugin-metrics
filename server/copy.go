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
	if _, err := b.FileExists(src); err != nil {
		return err
	}

	// create the dest parent if necessary
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}

	entries, listErr := b.ListDirectory(src)
	var pathError *fs.PathError
	if (listErr != nil && errors.As(listErr, &pathError)) || len(entries) == 0 {
		// in s3 we should check whether the enrty count is 0 because
		// the API doesn't return an error if the object is a file.
		// something to check further with https://mattermost.atlassian.net/browse/MM-57034
		//
		// For local filesstore, we know that we would get a path error and that
		// means that this is a single file. Alternatively, we can improve the API by adding
		// additional IsDirectory method.
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
		f, err := os.OpenFile(fileDest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660)
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
	newZipFile, cErr := os.OpenFile(compressedFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0750)
	if cErr != nil {
		return cErr
	}
	defer newZipFile.Close()

	zr := gzip.NewWriter(newZipFile)
	defer zr.Close()

	tw := tar.NewWriter(zr)
	defer tw.Close()

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

	return nil
}
