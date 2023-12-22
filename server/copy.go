package main

import (
	"os"
	"path/filepath"
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
