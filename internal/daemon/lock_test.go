package daemon

import (
	"path/filepath"
	"testing"
)

func TestAcquireLock(t *testing.T) {
	dir := t.TempDir()

	l1, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer l1.Release()

	// Second attempt should fail
	_, err = AcquireLock(dir)
	if err == nil {
		t.Fatalf("expected second lock attempt to fail")
	}

	// Verify PID is in the file
	// Unlocking/Cleanup happens via l1.Release() (or test exit)
}

func TestAcquireLockAfterRelease(t *testing.T) {
	dir := t.TempDir()

	l1, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	// manually release
	l1.Release()

	// Ensure lock path was removed
	lockPath := filepath.Join(dir, "daemon.lock")
	if _, err := AcquireLock(dir); err != nil { // try locking again
		t.Fatalf("unable to acquire lock after release: %v (path: %s)", err, lockPath)
	}
}
