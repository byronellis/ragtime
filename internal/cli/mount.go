package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	ragtimefs "github.com/byronellis/ragtime/internal/fs"
	"github.com/byronellis/ragtime/internal/project"
	"github.com/spf13/cobra"
)

func newMountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mount",
		Short: "Mount the ragtime filesystem",
		Long:  "Mount the ragtime FUSE filesystem at ~/.ragtime/fs (or --path). Foreground; press Ctrl+C to unmount.",
		RunE:  runMount,
	}
	cmd.Flags().String("path", "", "mount point (default: ~/.ragtime/fs)")
	return cmd
}

func newUmountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "umount",
		Short: "Unmount the ragtime filesystem",
		RunE:  runUmount,
	}
	cmd.Flags().String("path", "", "mount point (default: ~/.ragtime/fs)")
	return cmd
}

func defaultMountPath() (string, error) {
	g := project.GlobalDir()
	if g == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		g = home + "/.ragtime"
	}
	return g + "/fs", nil
}

func runMount(cmd *cobra.Command, args []string) error {
	mountPath, _ := cmd.Flags().GetString("path")
	if mountPath == "" {
		p, err := defaultMountPath()
		if err != nil {
			return err
		}
		mountPath = p
	}

	socketPath := flagSocket
	if socketPath == "" {
		cfg, err := config.Load(".")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		socketPath = cfg.Daemon.Socket
	}

	if err := daemon.EnsureRunning(socketPath); err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}

	rfs, err := ragtimefs.New(socketPath)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel on SIGINT/SIGTERM so the mount is cleanly unmounted
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Printf("Mounting ragtime filesystem at %s\n", mountPath)
	fmt.Println("Press Ctrl+C to unmount.")

	if err := rfs.Mount(ctx, mountPath); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	fmt.Println("\nUnmounted.")
	return nil
}

func runUmount(cmd *cobra.Command, args []string) error {
	mountPath, _ := cmd.Flags().GetString("path")
	if mountPath == "" {
		p, err := defaultMountPath()
		if err != nil {
			return err
		}
		mountPath = p
	}

	if err := ragtimefs.Unmount(mountPath); err != nil {
		return fmt.Errorf("unmount: %w", err)
	}
	fmt.Printf("Unmounted %s\n", mountPath)
	return nil
}
