package project

import (
	"os"
	"path/filepath"
)

// FindRoot walks up from the given directory to find the nearest .git directory,
// returning the project root path. Returns empty string if no git root is found.
func FindRoot(from string) string {
	dir, err := filepath.Abs(from)
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// RagtimeDir returns the per-project .ragtime directory path,
// or empty string if no git root is found.
func RagtimeDir(from string) string {
	root := FindRoot(from)
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".ragtime")
}

// GlobalDir returns the global ~/.ragtime directory path.
func GlobalDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ragtime")
}
