package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadMerge(t *testing.T) {
	// Setup a temporary environment
	dir := t.TempDir()
	
	// Temporarily change working directory to test merging local .memcp.yaml
	origWD, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origWD)

	// Create local .memcp.yaml
	localYAML := `
logging:
  level: "debug"
memory:
  context_window: 100
`
	err := os.WriteFile(".memcp.yaml", []byte(localYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write .memcp.yaml: %v", err)
	}

	// We don't stub os.UserConfigDir easily here, but we can rely on the fallback 
	// loading defaults and then merging the local config.
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify defaults are present
	if cfg.Persona.MaxCharsPerFile != 20000 {
		t.Errorf("expected default Persona.MaxCharsPerFile 20000, got %d", cfg.Persona.MaxCharsPerFile)
	}

	// Verify local overrides took effect
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected Logging.Level 'debug', got %q", cfg.Logging.Level)
	}
	if cfg.Memory.ContextWindow != 100 {
		t.Errorf("expected Memory.ContextWindow 100, got %d", cfg.Memory.ContextWindow)
	}
}

func TestResolvePaths(t *testing.T) {
	cfg := &Config{}
	cfg.Persona.SoulDir = "soul"
	cfg.Memory.DBPath = "data/db.sqlite"

	cfg.resolvePaths("/tmp/testbase")

	if cfg.Persona.SoulDir != filepath.Join("/tmp/testbase", "soul") {
		t.Errorf("expected resolved soul_dir, got %q", cfg.Persona.SoulDir)
	}
	if cfg.Memory.DBPath != filepath.Join("/tmp/testbase", "data/db.sqlite") {
		t.Errorf("expected resolved db_path, got %q", cfg.Memory.DBPath)
	}

	// Absolute paths should not be changed
	cfg.Persona.SoulDir = "/absolute/path"
	cfg.resolvePaths("/tmp/testbase")
	if cfg.Persona.SoulDir != "/absolute/path" {
		t.Errorf("absolute path changed: %q", cfg.Persona.SoulDir)
	}
}
