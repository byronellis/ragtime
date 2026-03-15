package config

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ReloadFunc is called when watched files change. It receives the list of changed paths.
type ReloadFunc func(paths []string)

// Watcher monitors configuration and rule directories for changes.
type Watcher struct {
	watcher  *fsnotify.Watcher
	callback ReloadFunc
	logger   *slog.Logger
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewWatcher creates a file watcher that calls the callback when files change.
func NewWatcher(callback ReloadFunc, logger *slog.Logger) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		watcher:  w,
		callback: callback,
		logger:   logger,
		done:     make(chan struct{}),
	}, nil
}

// Watch adds a directory to the watch list. Non-existent directories are silently skipped.
func (w *Watcher) Watch(dirs ...string) {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := w.watcher.Add(dir); err != nil {
			w.logger.Debug("skip watch", "dir", dir, "error", err)
		} else {
			w.logger.Debug("watching", "dir", dir)
		}
	}
}

// Start begins processing filesystem events. Call Stop to shut down.
func (w *Watcher) Start() {
	w.wg.Add(1)
	go w.loop()
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
	w.wg.Wait()
}

func (w *Watcher) loop() {
	defer w.wg.Done()

	// Debounce: collect changes over a short window before firing callback
	var (
		timer    *time.Timer
		pending  []string
		mu       sync.Mutex
	)

	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !isRelevant(event) {
				continue
			}

			mu.Lock()
			pending = append(pending, event.Name)
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(200*time.Millisecond, func() {
				mu.Lock()
				paths := pending
				pending = nil
				mu.Unlock()

				if len(paths) > 0 {
					w.logger.Info("config changed", "files", len(paths))
					w.callback(paths)
				}
			})
			mu.Unlock()

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("watch error", "error", err)
		}
	}
}

func isRelevant(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
		return false
	}
	ext := filepath.Ext(event.Name)
	return ext == ".yaml" || ext == ".yml" || ext == ".star"
}
