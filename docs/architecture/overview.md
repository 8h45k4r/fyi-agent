# FYI Agent Architecture

## Overview

FYI Agent is a zero-trust SASE (Secure Access Service Edge) client for Windows and macOS. It provides Zscaler-equivalent capabilities as an open-source solution under the Certifyi platform.

## 5-Layer Architecture

```
+--------------------------------------------------+
|  Layer 5: AI Policy Engine                        |
|  Risk scoring, rule evaluation, remote sync       |
+--------------------------------------------------+
|  Layer 4: Inspection Layer                        |
|  SSL/TLS inspection + DLP pattern matching        |
+--------------------------------------------------+
|  Layer 3: Traffic Steering                        |
|  PAC engine, VPN coexistence, split tunnel        |
+--------------------------------------------------+
|  Layer 2: Zero Trust Identity                     |
|  OIDC/SAML/cert auth, token refresh               |
+--------------------------------------------------+
|  Layer 1: TUN Interface                           |
|  Virtual network device, route management         |
+--------------------------------------------------+
```

## Package Structure

```
cmd/
  fyi-agent/
    main.go           # Shared entry point
    windows/           # Windows service (SCM)
    darwin/            # macOS launchd daemon
pkg/
  config/             # Configuration loading & validation
  tunnel/             # TUN device management
  zerotrust/          # Identity & authentication
  steering/pac/       # PAC proxy auto-config
  inspection/
    dlp/              # Data Loss Prevention
    ssl/              # SSL/TLS inspection
  policy/             # AI-driven policy engine
  transport/
    congestion/       # BBR congestion control
  diagnostics/        # Health checks & metrics
internal/
  captive/            # Captive portal detection
  watchdog/           # Process health monitoring
configs/
  agent.yaml          # Default configuration
```

## Key Design Decisions

### Platform Abstraction
The agent uses build tags and interface injection for platform-specific code. The `tunnel.Device` interface allows different TUN implementations per OS.

### Subsystem Lifecycle
Subsystems initialize in dependency order (identity -> tunnel -> steering -> inspection -> policy) and shut down in reverse. Each subsystem implements a `Start(ctx)` / `Stop()` pattern.

### Configuration
YAML-based configuration with environment variable overrides. Every config struct has a `DefaultConfig()` function for safe defaults.

### Error Handling
All errors are wrapped with package context (e.g., `tunnel: ...`). The watchdog monitors subsystem health and triggers restarts within configurable limits.

### Testing
Every public function has a test. Error paths and edge cases are tested explicitly. Table-driven tests are used where multiple input variations exist.

## OpenZiti Improvements

This agent addresses key OpenZiti issues:
- **Congestion control** (#2577): BBR implementation replacing naive windowing
- **Captive portal** detection and automatic tunnel pause/resume
- **JWT lifecycle**: Proactive token refresh before expiry
- **Multi-tenancy**: Policy engine supports per-user rule evaluation
- **HA failover**: Reconnect with exponential backoff

## Build & Run

```bash
make build       # Build for current platform
make test        # Run all tests
make lint        # Run linter
make build-all   # Cross-compile Windows + macOS
```
