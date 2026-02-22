package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ValidConfig(t *testing.T) {
	yaml := []byte(`
agent:
  name: test-agent
  tenant_id: tenant-123
  log_level: info
transport:
  controller_url: https://ctrl.certifyi.ai
  identity_file: /etc/fyi/identity.json
steering:
  pac_url: https://pac.certifyi.ai/proxy.pac
  split_tunnel: true
  bypass_list:
    - "*.local"
dlp:
  enabled: true
  patterns:
    - credit_card
captive:
  probe_url: http://captive.certifyi.ai/generate_204
`)

	cfg, err := Parse(yaml)
	require.NoError(t, err)
	assert.Equal(t, "test-agent", cfg.Agent.Name)
	assert.Equal(t, "tenant-123", cfg.Agent.TenantID)
	assert.Equal(t, "https://ctrl.certifyi.ai", cfg.Transport.ControllerURL)
	assert.True(t, cfg.Steering.SplitTunnel)
	assert.True(t, cfg.DLP.Enabled)
	assert.Len(t, cfg.Steering.BypassList, 1)
}

func TestParse_EmptyData(t *testing.T) {
	_, err := Parse([]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte("invalid: [yaml: broken"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config")
}

func TestParse_MissingTenantID(t *testing.T) {
	yaml := []byte(`
agent:
  name: test
transport:
  controller_url: https://ctrl.certifyi.ai
`)
	_, err := Parse(yaml)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id is required")
}

func TestParse_MissingControllerURL(t *testing.T) {
	yaml := []byte(`
agent:
  tenant_id: tenant-123
transport:
  identity_file: /etc/fyi/identity.json
`)
	_, err := Parse(yaml)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "controller_url is required")
}

func TestLoad_EmptyPath(t *testing.T) {
	_, err := Load("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path cannot be empty")
}

func TestLoad_NonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}
