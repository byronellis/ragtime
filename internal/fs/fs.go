// Package fs implements the ragtime FUSE filesystem.
// Requires FUSE-T (macOS) or libfuse (Linux).
//
// Mount point layout:
//
//	~/.ragtime/fs/
//	  sessions/<agent>-<session-id>/   live session data
//	  collections/<name>/              RAG index data
//	  agents/<shell-id>/               PTY shell state
//	  shells -> agents                 alias symlink
package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/project"
	"github.com/byronellis/ragtime/internal/rag"
	"github.com/byronellis/ragtime/internal/rag/providers"
	"github.com/winfsp/cgofuse/fuse"
)

// RagtimeFS holds runtime state for the mounted filesystem.
type RagtimeFS struct {
	daemon *daemonClient
	rag    *rag.Engine
	host   *fuse.FileSystemHost
}

// New creates a RagtimeFS from the given daemon socket path.
func New(socketPath string) (*RagtimeFS, error) {
	cfg, err := config.Load(".")
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	var ragEngine *rag.Engine
	if dirs := collectIndexDirs(); len(dirs) > 0 {
		provider := providers.NewOllama(cfg.Embeddings.Endpoint, cfg.Embeddings.Model)
		ragEngine = rag.NewEngine(dirs, provider, nil)
	}

	return &RagtimeFS{
		daemon: newDaemonClient(socketPath),
		rag:    ragEngine,
	}, nil
}

// Mount mounts the filesystem at mountPath and blocks until ctx is done or externally unmounted.
func (rfs *RagtimeFS) Mount(ctx context.Context, mountPath string) error {
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		return fmt.Errorf("create mount point: %w", err)
	}

	impl := &ragtimeImpl{rfs: rfs}
	host := fuse.NewFileSystemHost(impl)
	host.SetCapReaddirPlus(true)
	rfs.host = host

	errCh := make(chan error, 1)
	go func() {
		ok := host.Mount(mountPath, []string{"-o", "fsname=ragtime,volname=ragtime"})
		if !ok {
			errCh <- fmt.Errorf("mount failed (is FUSE-T or macFUSE installed?)")
		} else {
			errCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		host.Unmount()
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// Unmount unmounts the filesystem at mountPath from an external process.
func Unmount(mountPath string) error {
	return platformUnmount(mountPath)
}

func collectIndexDirs() []string {
	var dirs []string
	if g := project.GlobalDir(); g != "" {
		dirs = append(dirs, filepath.Join(g, "indexes"))
	}
	cwd, _ := os.Getwd()
	if p := project.RagtimeDir(cwd); p != "" {
		dirs = append(dirs, filepath.Join(p, "indexes"))
	}
	return dirs
}
