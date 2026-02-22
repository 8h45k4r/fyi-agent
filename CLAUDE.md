# CLAUDE.md — FYI Agent (Certifyi Platform)

## Project Overview
FYI Agent is a zero-trust SASE agent for Windows and macOS, built on
OpenZiti's architecture. It adds Zscaler-class capabilities: PAC-based
traffic steering, SSL/TLS inspection, inline DLP, AI-driven policy
engine, captive portal handling, and MDM deployment support.

## Tech Stack
- **Core transport**: Go (forked from openziti/sdk-golang)
- **Windows agent**: C# (.NET 8) + wintun TUN driver
- **macOS agent**: Swift + Network Extension (utun)
- **AI policy engine**: Go + ONNX Runtime
- **DLP engine**: Go + regex + distilBERT NER
- **Build**: Makefile, GoReleaser, MSI (Windows), .pkg (macOS)
- **Tests**: Go testing, testify, mockery

## Project Structure
```
/cmd/fyi-agent/        — Agent entry points (Windows/macOS)
/pkg/transport/        — Zero-trust mesh (OpenZiti fork)
/pkg/steering/         — PAC engine, split-tunnel, VPN coexistence
/pkg/inspection/       — SSL proxy, DLP inline engine
/pkg/ai/               — On-device ML policy engine
/pkg/diagnostics/      — Self-service troubleshooting tools
/pkg/mdm/              — MDM enrollment, certificate management
/internal/tun/         — TUN driver wrappers (wintun/utun)
/internal/captive/     — Captive portal detection
/desktop-win/          — Windows UI (C#/WPF)
/desktop-mac/          — macOS UI (SwiftUI)
```

## Engineering Preferences (STRICT — apply to ALL code)
- DRY is important — flag repetition aggressively
- Well-tested code is non-negotiable; too many tests > too few
- "Engineered enough" — not under-engineered (fragile) or over-engineered (premature abstraction)
- Handle MORE edge cases, not fewer; thoughtfulness > speed
- Explicit over clever — always
- Every public function has a test
- Every error path has a test
- No TODO without a linked issue number

## Review Workflow (for all code changes)
BEFORE making any code changes, choose a review mode:
- BIG CHANGE: Interactive, one section at a time (Architecture -> Code Quality -> Tests -> Performance), max 4 issues per section
- SMALL CHANGE: Interactive, ONE issue per section

For EACH issue found:
1. Describe the problem with file + line references
2. Present 2-3 options (including "do nothing")
3. For each option: effort, risk, impact, maintenance burden
4. Give opinionated recommendation mapped to preferences above
5. NUMBER issues, LETTER options (e.g., Issue 1A, 1B, 1C)
6. ASK for input before proceeding

## Review Sections
### 1. Architecture Review
- System design and component boundaries
- Dependency graph and coupling
- Data flow and bottlenecks
- Scaling and single points of failure
- Security architecture (auth, data access, API boundaries)

### 2. Code Quality Review
- Organization and module structure
- DRY violations (aggressive)
- Error handling and missing edge cases (explicit callouts)
- Technical debt hotspots
- Over/under-engineering assessment

### 3. Test Review
- Coverage gaps (unit, integration, e2e)
- Assertion strength
- Missing edge case coverage
- Untested failure modes and error paths

### 4. Performance Review
- N+1 queries and data access patterns
- Memory usage concerns
- Caching opportunities
- High-complexity code paths

## Build & Test Commands
```bash
go build ./cmd/fyi-agent/...
go test ./... -v -race -count=1
go test ./... -coverprofile=coverage.out
golangci-lint run
```

## Commit Convention
```
feat(steering): add PAC file dynamic reload
fix(transport): resolve congestion collapse on 4+ flows
test(dlp): add edge cases for PII detection with unicode
```
