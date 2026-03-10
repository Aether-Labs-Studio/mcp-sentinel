package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgs_ExplicitRules(t *testing.T) {
	rulesPath, disableTelemetry, hubMode, subArgs := parseArgs([]string{
		"--rules", "custom.json",
		"npx", "-y", "@modelcontextprotocol/server-filesystem", ".",
	})
	if rulesPath != "custom.json" {
		t.Errorf("expected rulesPath=custom.json, got %q", rulesPath)
	}
	if disableTelemetry {
		t.Error("expected disableTelemetry=false when flag not set")
	}
	if hubMode != "" {
		t.Errorf("expected empty hubMode, got %q", hubMode)
	}
	if len(subArgs) != 4 || subArgs[0] != "npx" {
		t.Errorf("unexpected subArgs: %v", subArgs)
	}
}

func TestParseArgs_NoFlags(t *testing.T) {
	rulesPath, disableTelemetry, hubMode, subArgs := parseArgs([]string{"npx", "server"})
	if rulesPath != "" {
		t.Errorf("expected empty rulesPath, got %q", rulesPath)
	}
	if disableTelemetry {
		t.Error("expected disableTelemetry=false when flag not set")
	}
	if hubMode != "" {
		t.Errorf("expected empty hubMode, got %q", hubMode)
	}
	if len(subArgs) != 2 {
		t.Errorf("unexpected subArgs: %v", subArgs)
	}
}

func TestParseArgs_DisableTelemetry(t *testing.T) {
	_, disableTelemetry, hubMode, subArgs := parseArgs([]string{
		"--disable-telemetry",
		"npx", "server",
	})
	if !disableTelemetry {
		t.Error("expected disableTelemetry=true")
	}
	if hubMode != "" {
		t.Errorf("expected empty hubMode, got %q", hubMode)
	}
	for _, a := range subArgs {
		if a == "--disable-telemetry" {
			t.Error("--disable-telemetry should not appear in subArgs")
		}
	}
	if len(subArgs) != 2 || subArgs[0] != "npx" {
		t.Errorf("unexpected subArgs: %v", subArgs)
	}
}

func TestParseArgs_DisableTelemetryWithOtherFlags(t *testing.T) {
	rulesPath, disableTelemetry, hubMode, subArgs := parseArgs([]string{
		"--rules", "my.json",
		"--disable-telemetry",
		"node", "server.js",
	})
	if rulesPath != "my.json" {
		t.Errorf("expected rulesPath=my.json, got %q", rulesPath)
	}
	if !disableTelemetry {
		t.Error("expected disableTelemetry=true")
	}
	if hubMode != "" {
		t.Errorf("expected empty hubMode, got %q", hubMode)
	}
	if len(subArgs) != 2 || subArgs[0] != "node" {
		t.Errorf("unexpected subArgs: %v", subArgs)
	}
}

func TestParseArgs_TelemetryHubMode(t *testing.T) {
	_, _, hubMode, subArgs := parseArgs([]string{
		"--telemetry-hub-mode", "on_update",
		"npx", "server",
	})
	if hubMode != "on_update" {
		t.Errorf("expected hubMode=on_update, got %q", hubMode)
	}
	for _, a := range subArgs {
		if a == "--telemetry-hub-mode" || a == "on_update" {
			t.Error("telemetry hub mode args should not appear in subArgs")
		}
	}
	if len(subArgs) != 2 || subArgs[0] != "npx" {
		t.Errorf("unexpected subArgs: %v", subArgs)
	}
}

func TestResolveCmd_KnownCommand(t *testing.T) {
	resolved := resolveCmd("ls")
	if resolved == "" {
		t.Fatal("expected non-empty path for 'ls'")
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path for 'ls', got %q", resolved)
	}
}

func TestResolveCmd_UnknownCommand(t *testing.T) {
	name := "this-command-does-not-exist-xyz-sentinel-123"
	resolved := resolveCmd(name)
	if resolved != name {
		t.Errorf("expected %q returned as-is, got %q", name, resolved)
	}
}

func TestApplyDefaultFilesystemRoot_AppendsWhenMissing(t *testing.T) {
	args := []string{"npx", "-y", "@modelcontextprotocol/server-filesystem"}
	got := applyDefaultFilesystemRoot(args, "/tmp/project")
	if len(got) != 4 || got[3] != "/tmp/project" {
		t.Fatalf("expected root appended, got %v", got)
	}
}

func TestApplyDefaultFilesystemRoot_DoesNotAppendWhenAlreadyPresent(t *testing.T) {
	args := []string{"npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp/project"}
	got := applyDefaultFilesystemRoot(args, "/tmp/other")
	if len(got) != 4 || got[3] != "/tmp/project" {
		t.Fatalf("expected explicit root preserved, got %v", got)
	}
}

func TestApplyDefaultFilesystemRoot_IgnoresOtherCommands(t *testing.T) {
	args := []string{"node", "server.js"}
	got := applyDefaultFilesystemRoot(args, "/tmp/project")
	if len(got) != 2 {
		t.Fatalf("expected args unchanged, got %v", got)
	}
}

func TestResolveRulesPath_ExplicitAlwaysWins(t *testing.T) {
	path, err := resolveRulesPath("/some/explicit/rules.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/some/explicit/rules.json" {
		t.Errorf("expected explicit path returned as-is, got %q", path)
	}
}

func TestResolveRulesPath_UserDefault(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	sentinelDir := filepath.Join(homeDir, ".sentinel")
	if err := os.MkdirAll(sentinelDir, 0700); err != nil {
		t.Fatalf("cannot create sentinel dir: %v", err)
	}
	defaultPath := filepath.Join(sentinelDir, "rules.json")
	_, statErr := os.Stat(defaultPath)
	if statErr != nil {
		if err := os.WriteFile(defaultPath, []byte(`{"blocked_tools":[],"blocked_paths":[]}`), 0600); err != nil {
			t.Fatalf("cannot write temp rules file: %v", err)
		}
		defer os.Remove(defaultPath)
	}

	path, err := resolveRulesPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != defaultPath {
		t.Errorf("expected %q, got %q", defaultPath, path)
	}
}

func TestResolveRulesPath_ErrorWhenNoneFound(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	defaultPath := filepath.Join(homeDir, ".sentinel", "rules.json")
	if _, err := os.Stat(defaultPath); err == nil {
		t.Skip("~/.sentinel/rules.json exists — skipping 'not found' test")
	}

	orig, _ := os.Getwd()
	defer os.Chdir(orig) //nolint:errcheck
	os.Chdir("/")

	_, err := resolveRulesPath("")
	if err == nil {
		t.Error("expected error when no rules file found, got nil")
	}
}
