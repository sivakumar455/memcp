package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// LockFile represents an exclusive lock on the daemon.
type LockFile struct {
	path string
	file *os.File
}

// AcquireLock attempts to obtain an exclusive file lock.
func AcquireLock(dataDir string) (*LockFile, error) {
	lockPath := filepath.Join(dataDir, "daemon.lock")
	
	// Open or create the file
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}

	// Try to get exclusive non-blocking lock
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("daemon is already running (locked): %w", err)
	}

	// Write our PID into the lock file for debugging
	pid := os.Getpid()
	file.Truncate(0)
	file.Seek(0, 0)
	file.WriteString(strconv.Itoa(pid))

	return &LockFile{
		path: lockPath,
		file: file, // Keep file open to hold the lock
	}, nil
}

// Release correctly removes the lock file and releases the syscall flock.
func (l *LockFile) Release() error {
	defer l.file.Close()
	
	// Unlocking is technically implicit on close, but we can be explicit
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	
	return os.Remove(l.path)
}
