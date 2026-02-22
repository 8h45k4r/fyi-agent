//go:build darwin

// Package main provides the macOS launchd daemon entry point for the FYI Agent.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/8h45k4r/fyi-agent/pkg/config"
)

var version = "dev"

func main() {
	log.Printf("FYI Agent %s starting (macOS)...\n", version)

	cfgPath := os.Getenv("FYI_CONFIG")
	if cfgPath == "" {
		cfgPath = "/Library/FYI/agent.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...\n", sig)
		cancel()
	}()

	if err := run(ctx, cfg); err != nil {
		log.Fatalf("Agent error: %v", err)
	}

	log.Println("FYI Agent stopped.")
}

func run(ctx context.Context, cfg *config.Config) error {
	log.Printf("Agent configured for tenant: %s\n", cfg.Agent.TenantID)
	log.Printf("Controller: %s\n", cfg.Transport.ControllerURL)

	// TODO(#7): Initialize utun Network Extension
	// TODO(#8): Initialize transport layer
	// TODO(#9): Initialize PAC steering engine
	// TODO(#10): Initialize DLP inspection proxy
	// TODO(#11): Initialize captive portal detector
	// TODO(#12): Register for NSWorkspace sleep/wake notifications

	<-ctx.Done()
	return fmt.Errorf("agent context cancelled: %w", ctx.Err())
}
