package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRoot(t *testing.T) {
	// Create a temp dir structure: tmp/repo/.git/ and tmp/repo/sub/dir/
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	gitDir := filepath.Join(repoDir, ".git")
	subDir := filepath.Join(repoDir, "sub", "dir")

	os.MkdirAll(gitDir, 0o755)
	os.MkdirAll(subDir, 0o755)

	got := FindRoot(subDir)
	if got != repoDir {
		t.Errorf("FindRoot(%q) = %q, want %q", subDir, got, repoDir)
	}

	// From repo root itself
	got = FindRoot(repoDir)
	if got != repoDir {
		t.Errorf("FindRoot(%q) = %q, want %q", repoDir, got, repoDir)
	}

	// From a dir with no git
	noGit := filepath.Join(tmp, "nogit")
	os.MkdirAll(noGit, 0o755)
	got = FindRoot(noGit)
	if got != "" {
		t.Errorf("FindRoot(%q) = %q, want empty", noGit, got)
	}
}

func TestRagtimeDir(t *testing.T) {
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)

	got := RagtimeDir(repoDir)
	want := filepath.Join(repoDir, ".ragtime")
	if got != want {
		t.Errorf("RagtimeDir = %q, want %q", got, want)
	}
}
