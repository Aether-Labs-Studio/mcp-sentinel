package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// File mirrors the top-level structure of a rules.json file.
type File struct {
	BlockedTools []string `json:"blocked_tools"`
	BlockedPaths []string `json:"blocked_paths"`
}

// CompiledRule pairs a precompiled regular expression with a human-readable description.
type CompiledRule struct {
	Pattern     *regexp.Regexp
	Description string
}

// Rules holds all compiled security rules ready for O(1) runtime dispatch.
type Rules struct {
	BlockedTools []string
	BlockedPaths []CompiledRule
}

// SentinelConfig holds user-level defaults read from ~/.sentinel/config.json.
// All fields are optional — absent or malformed file results in zero-value struct.
type SentinelConfig struct {
	DefaultFilesystemRoot string `json:"default_filesystem_root,omitempty"`
	TelemetryEnabled *bool  `json:"telemetry_enabled,omitempty"`
	TelemetryHubMode string `json:"telemetry_hub_mode,omitempty"`
}

// LoadSentinelConfig reads ~/.sentinel/config.json if it exists.
// The file is optional: if absent, an empty SentinelConfig is returned with no error.
// If the file exists but contains invalid JSON, a warning error is returned alongside
// the empty config so the caller can log it without failing.
func LoadSentinelConfig() (SentinelConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return SentinelConfig{}, nil // no home dir — silently skip
	}
	path := filepath.Join(homeDir, ".sentinel", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return SentinelConfig{}, nil // file absent — silently skip
	}
	var cfg SentinelConfig
	if err := unmarshalSentinelConfig(data, &cfg); err != nil {
		return SentinelConfig{}, err
	}
	return cfg, nil
}

// unmarshalSentinelConfig parses JSON bytes into cfg.
// Separated from LoadSentinelConfig to allow unit tests without disk I/O.
func unmarshalSentinelConfig(data []byte, cfg *SentinelConfig) error {
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: ~/.sentinel/config.json is malformed: %w", err)
	}
	return nil
}

// LoadRules reads path, parses the JSON, compiles all regular expressions, and
// returns ready-to-use Rules. Returns a non-nil error if the file cannot be
// read, is invalid JSON, or any pattern fails to compile.
func LoadRules(path string) (*Rules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read rules file %q: %w", path, err)
	}

	var f File
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("config: invalid JSON in %q: %w", path, err)
	}

	rules := &Rules{
		BlockedTools: f.BlockedTools,
		BlockedPaths: make([]CompiledRule, 0, len(f.BlockedPaths)),
	}

	for _, p := range f.BlockedPaths {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("config: invalid blocked_path pattern %q: %w", p, err)
		}
		rules.BlockedPaths = append(rules.BlockedPaths, CompiledRule{Pattern: re, Description: p})
	}

	return rules, nil
}
