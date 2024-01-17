package main

import (
	"archive/zip"
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

func zipDirectory(sourceDir, zipFile string) error {
	// Create a new zip file
	newZipFile, err := os.Create(zipFile)
	if err != nil {
		return err
	}
	defer newZipFile.Close()

	// Create a new zip writer
	zipWriter := zip.NewWriter(newZipFile)
	defer zipWriter.Close()

	// Walk through the directory and add files to the zip archive
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create a file header based on the file info
		fileHeader, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		// Modify the file header to use relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		fileHeader.Name = relPath

		// Check if the file is a directory or a regular file
		if info.IsDir() {
			// Skip directories as they will be created implicitly
			return nil
		}

		// Create an entry in the zip file
		fileWriter, err := zipWriter.CreateHeader(fileHeader)
		if err != nil {
			return err
		}

		// Open the file for reading
		fileToZip, err := os.Open(path)
		if err != nil {
			return err
		}
		defer fileToZip.Close()

		// Copy file contents to the zip entry
		_, err = io.Copy(fileWriter, fileToZip)
		if err != nil {
			return err
		}

		return nil
	})

	return err
}
