package store

import (
	"os"
	"path/filepath"
)

func parentDir(path string) string {
	return filepath.Dir(path)
}

func mkdirAll(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
