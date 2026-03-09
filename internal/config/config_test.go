package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("cannot write temp config: %v", err)
	}
	return path
}

func TestLoadSentinelConfig_AbsentFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	_, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err == nil {
		t.Fatal("expected file to be absent")
	}
	cfg, err := LoadSentinelConfig()
	if err != nil {
		t.Errorf("expected no error for absent config, got: %v", err)
	}
	_ = cfg
}

func TestLoadSentinelConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir, `{not valid json}`)

	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var cfg SentinelConfig
	if err := unmarshalSentinelConfig(data, &cfg); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLoadSentinelConfig_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir, `{}`)

	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var cfg SentinelConfig
	if err := unmarshalSentinelConfig(data, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelemetryEnabled != nil {
		t.Error("expected nil TelemetryEnabled when field is absent")
	}
	if cfg.TelemetryHubMode != "" {
		t.Errorf("expected empty TelemetryHubMode, got %q", cfg.TelemetryHubMode)
	}
}

func TestLoadSentinelConfig_TelemetryEnabledFalse(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir, `{"telemetry_enabled": false}`)

	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var cfg SentinelConfig
	if err := unmarshalSentinelConfig(data, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelemetryEnabled == nil {
		t.Fatal("expected TelemetryEnabled to be set")
	}
	if *cfg.TelemetryEnabled {
		t.Error("expected TelemetryEnabled=false")
	}
}

func TestLoadSentinelConfig_TelemetryEnabledTrue(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir, `{"telemetry_enabled": true}`)

	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var cfg SentinelConfig
	if err := unmarshalSentinelConfig(data, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelemetryEnabled == nil {
		t.Fatal("expected TelemetryEnabled to be set")
	}
	if !*cfg.TelemetryEnabled {
		t.Error("expected TelemetryEnabled=true")
	}
}

func TestLoadSentinelConfig_TelemetryHubMode(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir, `{"telemetry_hub_mode": "on_update"}`)

	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var cfg SentinelConfig
	if err := unmarshalSentinelConfig(data, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelemetryHubMode != "on_update" {
		t.Errorf("expected telemetry_hub_mode=on_update, got %q", cfg.TelemetryHubMode)
	}
}

func TestLoadRules_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	content := `{"blocked_tools":[],"blocked_paths":[],"ipi_patterns":[]}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("cannot write temp rules file: %v", err)
	}

	_, err := LoadRules(path)
	if err == nil {
		t.Fatal("expected unknown field error for ipi_patterns, got nil")
	}
}
