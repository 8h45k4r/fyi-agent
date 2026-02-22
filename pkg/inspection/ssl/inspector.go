// Package ssl implements TLS/SSL traffic inspection with certificate
// generation, domain bypass lists, and minimum TLS version enforcement.
package ssl

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// Config holds SSL inspection configuration.
type Config struct {
	Enabled       bool     `yaml:"enabled"`
	CACertFile    string   `yaml:"ca_cert_file"`
	CAKeyFile     string   `yaml:"ca_key_file"`
	BypassDomains []string `yaml:"bypass_domains"`
	MinTLSVersion string   `yaml:"min_tls_version"`
}

// DefaultConfig returns sensible SSL inspection defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		MinTLSVersion: "1.2",
		BypassDomains: []string{"localhost"},
	}
}

// Inspector performs SSL/TLS inspection on intercepted traffic.
type Inspector struct {
	cfg           Config
	logger        *slog.Logger
	minVersion    uint16
	bypassDomains map[string]bool
	wildcards     []string
	mu            sync.RWMutex
}

// New creates a new SSL inspector.
func New(cfg Config, logger *slog.Logger) (*Inspector, error) {
	if logger == nil {
		logger = slog.Default()
	}

	minVer, err := parseTLSVersion(cfg.MinTLSVersion)
	if err != nil {
		return nil, err
	}

	i := &Inspector{
		cfg:           cfg,
		logger:        logger,
		minVersion:    minVer,
		bypassDomains: make(map[string]bool),
	}

	for _, domain := range cfg.BypassDomains {
		d := strings.ToLower(strings.TrimSpace(domain))
		if strings.HasPrefix(d, "*.") {
			i.wildcards = append(i.wildcards, d[2:])
		} else {
			i.bypassDomains[d] = true
		}
	}

	logger.Info("SSL inspector initialized",
		"min_tls", cfg.MinTLSVersion,
		"bypass_count", len(cfg.BypassDomains),
	)
	return i, nil
}

// ShouldInspect returns true if traffic to the given domain should be inspected.
func (i *Inspector) ShouldInspect(domain string) bool {
	if !i.cfg.Enabled {
		return false
	}

	d := strings.ToLower(strings.TrimSpace(domain))

	i.mu.RLock()
	defer i.mu.RUnlock()

	// Exact match
	if i.bypassDomains[d] {
		return false
	}

	// Wildcard match
	for _, wc := range i.wildcards {
		if strings.HasSuffix(d, "."+wc) || d == wc {
			return false
		}
	}

	return true
}

// MinTLSVersion returns the configured minimum TLS version.
func (i *Inspector) MinTLSVersion() uint16 {
	return i.minVersion
}

// AddBypassDomain adds a domain to the bypass list at runtime.
func (i *Inspector) AddBypassDomain(domain string) {
	d := strings.ToLower(strings.TrimSpace(domain))
	i.mu.Lock()
	defer i.mu.Unlock()

	if strings.HasPrefix(d, "*.") {
		i.wildcards = append(i.wildcards, d[2:])
	} else {
		i.bypassDomains[d] = true
	}
	i.logger.Info("bypass domain added", "domain", domain)
}

// RemoveBypassDomain removes a domain from the bypass list.
func (i *Inspector) RemoveBypassDomain(domain string) {
	d := strings.ToLower(strings.TrimSpace(domain))
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.bypassDomains, d)
	i.logger.Info("bypass domain removed", "domain", domain)
}

func parseTLSVersion(v string) (uint16, error) {
	switch strings.TrimSpace(v) {
	case "1.0":
		return tls.VersionTLS10, nil
	case "1.1":
		return tls.VersionTLS11, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("ssl: unsupported TLS version %q", v)
	}
}
