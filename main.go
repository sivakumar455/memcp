// memcp is an agent memory system for MCP (Model Context Protocol).
// It provides persistent memory, persona/soul files, skill evolution,
// and optional background daemon capabilities for AI coding assistants.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/daemon"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/gateway"
	"github.com/sivakumar455/memcp/internal/logger"
	mcpserver "github.com/sivakumar455/memcp/internal/mcp"
	"github.com/sivakumar455/memcp/internal/observation"
	"github.com/sivakumar455/memcp/internal/shim"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "memcp",
		Short: "Agent memory system for MCP",
		Long: `memcp provides persistent memory, persona/soul files, skill evolution,
and background task management for AI coding assistants via MCP.`,
		RunE: runStandalone,
		Args: cobra.ArbitraryArgs, // allows passing arbitrary args after -- to shim
	}

	rootCmd.Flags().String("config", "", "Config name (overrides MEMCP_CONFIG env var)")
	rootCmd.Flags().String("data-dir", "", "Data directory (overrides MEMCP_DATA_DIR env var)")
	rootCmd.Flags().Bool("daemon", false, "Enable daemon add-on (background task polling)")
	rootCmd.Flags().Bool("shim", false, "Run in shim mode (transparent observation)")
	rootCmd.Flags().String("name", "", "Backend name for shim mode")

	rootCmd.Flags().Bool("http", false, "Start the HTTP Gateway server on 127.0.0.1:8787")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("memcp %s\n", Version)
		},
	}
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runStandalone(cmd *cobra.Command, args []string) error {
	// Determine data directory
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir == "" {
		dataDir = os.Getenv("MEMCP_DATA_DIR")
	}

	// Override config name from flag
	if cfgName, _ := cmd.Flags().GetString("config"); cfgName != "" {
		os.Setenv("MEMCP_CONFIG", cfgName)
	}

	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			dataDir = filepath.Join(home, ".memcp")
		} else {
			dataDir = "."
		}
	}

	// Load configuration
	cfg, err := config.Load(dataDir)
	if err != nil {
		fmt.Printf("Warning: failed to load config: %v\n", err) // Keep fmt for early setup issues
	}

	// Initialize structured logging system
	logger.Setup(cfg.Logging.Level, cfg.Logging.Dir)

	// Check for shim mode
	isShim, _ := cmd.Flags().GetBool("shim")
	if isShim {
		backendName, _ := cmd.Flags().GetString("name")
		if backendName == "" && len(args) > 0 {
			backendName = args[0]
		}

		slog.Info("memcp starting", "version", Version, "mode", "shim", "backend", backendName)

		eng, err := engine.NewObserverOnly(cfg)
		if err != nil {
			return fmt.Errorf("initializing observer engine: %w", err)
		}
		defer eng.Close()

		obs := observation.New(eng.Store, eng.MemMgr, cfg.Observation, nil)
		return shim.RunShim(args, obs, backendName)
	}

	slog.Info("memcp starting", "version", Version, "mode", "standalone")

	// Initialize engine
	eng, err := engine.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing engine: %w", err)
	}
	defer eng.Close()

	// Check for daemon mode
	isDaemon, _ := cmd.Flags().GetBool("daemon")
	if isDaemon || cfg.Daemon.Enabled || os.Getenv("MEMCP_DAEMON") == "true" {
		lock, err := daemon.AcquireLock(dataDir)
		if err != nil {
			slog.Warn("skipping daemon scheduler", "error", err)
		} else {
			defer lock.Release()
			slog.Info("starting daemon scheduler")
			sched := daemon.NewScheduler(cfg.Daemon, eng.Store, eng.MemMgr)
			// we register the dummy watcher
			sched.Register(daemon.NewDummyWatcher())

			go sched.Run(context.Background())
		}
	}

	// Check for HTTP Gateway
	isHTTP, _ := cmd.Flags().GetBool("http")
	if isHTTP || cfg.Gateway.Enabled {
		// Prepare gateway, background
		gw := gateway.NewServer(cfg.Gateway, eng.Store, eng.MemMgr, eng.Persona, eng.EvoEngine)
		go func() {
			if err := gw.Start(); err != nil && err != http.ErrServerClosed {
				slog.Error("gateway start failed", "error", err)
				os.Exit(1)
			}
		}()
	}

	// Create and run MCP server
	srv := mcpserver.New(eng, Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutting down...")
		cancel()
	}()

	return srv.Run(ctx)
}
