package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

// DaemonLock uses an exclusive file lock (flock) to ensure only one memcp
// process runs the daemon scheduler.
type DaemonLock struct {
	file *os.File
	path string
}

// TryAcquireDaemonLock attempts a non-blocking exclusive lock on
// <dataDir>/daemon.lock. Returns (lock, true) on success. If another process
// holds the lock, returns (nil, false).
func TryAcquireDaemonLock(dataDir string) (*DaemonLock, bool) {
	lockPath := filepath.Join(dataDir, "daemon.lock")

	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		slog.Warn("Failed to create lock directory", "path", lockPath, "error", err)
		return nil, false
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		slog.Warn("Failed to open daemon lock file", "path", lockPath, "error", err)
		return nil, false
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, false
	}

	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Sync()

	slog.Info("Daemon lock acquired", "path", lockPath, "pid", os.Getpid())
	return &DaemonLock{file: f, path: lockPath}, true
}

// Release closes the lock file and releases the flock.
func (l *DaemonLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = l.file.Close()
	slog.Info("Daemon lock released", "path", l.path)
}
