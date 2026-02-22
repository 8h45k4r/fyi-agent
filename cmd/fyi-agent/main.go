// Package main is the shared entry point for the FYI Agent.
// It detects the platform at runtime and delegates to the appropriate
// platform-specific initialization (Windows service or macOS launchd).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/8h45k4r/fyi-agent/pkg/config"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("FYI Agent starting",
		"version", version,
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
		"go_version", runtime.Version(),
	)

	// Load configuration
	cfgPath := getConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Warn("failed to load config, using defaults",
			"path", cfgPath,
			"error", err,
		)
		cfg = config.DefaultConfig()
	}

	logger.Info("configuration loaded",
		"log_level", cfg.LogLevel,
		"data_dir", cfg.DataDir,
	)

	// Create root context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	// Run the agent
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("agent exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("FYI Agent stopped gracefully")
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	// TODO(#1): Initialize subsystems in dependency order:
	// 1. Identity (Zero Trust authentication)
	// 2. Tunnel (TUN device)
	// 3. Steering (PAC engine)
	// 4. Inspection (SSL + DLP)
	// 5. Policy engine
	// 6. Watchdog
	// 7. Diagnostics server

	logger.Info("all subsystems initialized, entering main loop")

	// Block until context is cancelled
	<-ctx.Done()
	logger.Info("shutdown signal received, stopping subsystems")

	// TODO(#1): Shutdown subsystems in reverse order
	return nil
}

func getConfigPath() string {
	// Check environment variable first
	if p := os.Getenv("FYI_AGENT_CONFIG"); p != "" {
		return p
	}

	// Platform-specific default paths
	switch runtime.GOOS {
	case "darwin":
		return "/etc/fyi-agent/agent.yaml"
	case "windows":
		return fmt.Sprintf("%s\\fyi-agent\\agent.yaml", os.Getenv("PROGRAMDATA"))
	default:
		return "/etc/fyi-agent/agent.yaml"
	}
}
