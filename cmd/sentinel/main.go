package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aether-labs-studio/mcp-sentinel/internal/config"
	"github.com/aether-labs-studio/mcp-sentinel/internal/logx"
	"github.com/aether-labs-studio/mcp-sentinel/internal/policy"
	"github.com/aether-labs-studio/mcp-sentinel/internal/pump"
	"github.com/aether-labs-studio/mcp-sentinel/internal/telemetry"
)

const (
	gracePeriod   = 5 * time.Second
	telemetryAddr = "127.0.0.1:7438"
)

// parseArgs extracts Sentinel flags and subprocess arguments from os.Args[1:].
// Supported flags: --rules <path>, --disable-telemetry,
// --telemetry-hub-mode <relay|on_update|always_takeover>.
// Returns the rules path (empty if unspecified), whether telemetry
// is disabled, telemetry hub mode (empty if unspecified), and the remaining args.
func parseArgs(args []string) (rulesPath string, disableTelemetry bool, hubMode string, subArgs []string) {
	subArgs = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		if args[i] == "--rules" && i+1 < len(args) {
			rulesPath = args[i+1]
			i++
			continue
		}
		if args[i] == "--disable-telemetry" {
			disableTelemetry = true
			continue
		}
		if args[i] == "--telemetry-hub-mode" && i+1 < len(args) {
			hubMode = args[i+1]
			i++
			continue
		}
		subArgs = append(subArgs, args[i])
	}
	return
}

// resolveRulesPath determines the final rules file path using the following lookup order:
//  1. --rules <path> explicit flag (always wins)
//  2. ~/.sentinel/rules.json (user default)
//  3. ./rules.json (development fallback)
//
// Returns an error if none of the candidates exist on disk.
func resolveRulesPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	candidates := []string{"./rules.json"} // lowest priority first; overwritten below

	if homeDir, err := os.UserHomeDir(); err == nil {
		candidates = []string{
			filepath.Join(homeDir, ".sentinel", "rules.json"),
			"./rules.json",
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf(
		"no rules file found; create ~/.sentinel/rules.json or use --rules <path>",
	)
}

// resolveCmd resolves the full path of an executable.
// It first tries exec.LookPath (searches the process's current PATH).
// If that fails — common when MCP clients do not inherit the user's full PATH,
// as happens with version managers (nvm, rbenv, pyenv) on macOS — it falls back
// to a login shell that loads the user's profile (~/.bash_profile, ~/.zshrc, etc.).
// The name is passed as a positional argument ($1) to prevent shell injection.
// Returns the original name if neither method resolves it (intentional fail-open:
// exec.Command will emit a clear error if the command truly does not exist).
func resolveCmd(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	// Fallback: login shell. $1 prevents direct interpolation of name into the shell string.
	out, err := exec.Command("/bin/bash", "-l", "-c", `command -v "$1"`, "_", name).Output()
	if err == nil {
		if resolved := strings.TrimSpace(string(out)); resolved != "" {
			logx.Debugf("sentinel: %q not found in PATH; resolved via login shell → %s", name, resolved)
			return resolved
		}
	}
	return name // last resort: exec.Command will emit the appropriate error if it doesn't exist
}

func applyDefaultFilesystemRoot(subArgs []string, root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return subArgs
	}
	for i, arg := range subArgs {
		if arg != "@modelcontextprotocol/server-filesystem" {
			continue
		}
		if i == len(subArgs)-1 {
			logx.Debugf("sentinel: appending default_filesystem_root to server-filesystem → %s", root)
			return append(subArgs, root)
		}
		return subArgs
	}
	return subArgs
}

func main() {
	// Golden Rule: all observability output goes exclusively to stderr.
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	explicitRules, disableTelemetry, hubModeFlag, subArgs := parseArgs(os.Args[1:])
	if len(subArgs) == 0 {
		log.Fatal("sentinel: usage: mcp-sentinel [--rules <path>] [--disable-telemetry] [--telemetry-hub-mode <relay|on_update|always_takeover>] <command> [args...]")
	}

	hubModeRaw := hubModeFlag
	defaultFilesystemRoot := ""

	// Load user defaults from ~/.sentinel/config.json (optional).
	if sentinelCfg, err := config.LoadSentinelConfig(); err != nil {
		log.Printf("sentinel: WARNING — %v (skipping config.json)", err)
	} else {
		defaultFilesystemRoot = sentinelCfg.DefaultFilesystemRoot
		// Precedence: explicit --disable-telemetry flag wins over config.
		if !disableTelemetry && sentinelCfg.TelemetryEnabled != nil && !*sentinelCfg.TelemetryEnabled {
			disableTelemetry = true
			logx.Debugf("sentinel: telemetry disabled by ~/.sentinel/config.json (telemetry_enabled=false)")
		}
		// Precedence: explicit --telemetry-hub-mode flag wins over config.
		if hubModeRaw == "" && sentinelCfg.TelemetryHubMode != "" {
			hubModeRaw = sentinelCfg.TelemetryHubMode
			logx.Debugf("sentinel: telemetry hub mode from ~/.sentinel/config.json: %s", hubModeRaw)
		}
	}
	subArgs = applyDefaultFilesystemRoot(subArgs, defaultFilesystemRoot)

	hubMode, err := telemetry.ParseHubMode(hubModeRaw)
	if err != nil {
		log.Fatalf("sentinel: %v", err)
	}

	// Fail Secure: if no valid rules file is found, the proxy refuses to start.
	rulesPath, err := resolveRulesPath(explicitRules)
	if err != nil {
		log.Fatalf("sentinel: %v", err)
	}
	rules, err := config.LoadRules(rulesPath)
	if err != nil {
		log.Fatalf("sentinel: failed to load rules file: %v", err)
	}
	logx.Debugf("sentinel: rules loaded from %q (%d blocked_tools, %d blocked_paths)",
		rulesPath, len(rules.BlockedTools), len(rules.BlockedPaths))

	// Audit log: ~/.sentinel/audit.log (append-only JSONL).
	// Silent failure if home dir is unavailable — proxy continues without audit.
	auditPath := ""
	if homeDir, err := os.UserHomeDir(); err == nil {
		auditPath = filepath.Join(homeDir, ".sentinel", "audit.log")
	} else {
		logx.Debugf("sentinel: could not determine home dir: %v (audit log disabled)", err)
	}

	// Phase 4 — Telemetry: start as SSE hub if port is free, relay to primary hub otherwise.
	// Multiple sentinel instances share a single hub; all events flow to the same SSE stream.
	// --disable-telemetry skips the HTTP server and passes nil emitters to the policy engine.
	var emitter telemetry.Emitter
	if disableTelemetry {
		logx.Debugf("sentinel: telemetry disabled (--disable-telemetry)")
	} else {
		logx.Debugf("sentinel: telemetry hub mode: %s", hubMode)
		emitter = telemetry.StartOrRelay(telemetryAddr, auditPath, hubMode)
	}

	engine := policy.NewEngine(rules.BlockedPaths, rules.BlockedTools, emitter)

	cmd := exec.Command(resolveCmd(subArgs[0]), subArgs[1:]...)
	cmd.Stderr = os.Stderr

	childStdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("sentinel: failed to create stdin pipe: %v", err)
	}

	childStdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("sentinel: failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("sentinel: failed to start subprocess: %v", err)
	}
	logx.Debugf("sentinel: subprocess started (pid=%d) → %s", cmd.Process.Pid, subArgs[0])

	// Unconditional safety net (runs LAST due to LIFO defer order).
	defer func() {
		_ = cmd.Process.Kill()
	}()

	// ctx is cancelled when a signal is received, unblocking the pump loops
	// so they exit cleanly without waiting for the next scanner line.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case sig := <-sigCh:
			logx.Debugf("sentinel: signal received (%v), forwarding to subprocess...", sig)
			cancel() // unblock pump loops immediately
			if err := cmd.Process.Signal(sig); err != nil {
				log.Printf("sentinel: failed to forward signal (%v), forcing immediate Kill", err)
				_ = cmd.Process.Kill()
				return
			}
			time.AfterFunc(gracePeriod, func() {
				if err := cmd.Process.Kill(); err == nil {
					logx.Debugf("sentinel: grace period elapsed (%.0fs), Kill() executed", gracePeriod.Seconds())
				}
			})
		case <-done:
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// InboundPump: Client (os.Stdin) → MCP Server (childStdin) with Zero Trust policy.
	// Error responses for blocked requests are written back to os.Stdout (the client).
	go func() {
		defer wg.Done()
		defer childStdin.Close()
		pump.Inbound(ctx, childStdin, os.Stdout, os.Stdin, engine)
	}()

	// OutboundPump: MCP Server (childStdout) → Client (os.Stdout) as transparent passthrough.
	go func() {
		defer wg.Done()
		pump.Outbound(ctx, os.Stdout, childStdout)
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		log.Printf("sentinel: subprocess exited with error: %v", err)
	}
	logx.Debugf("sentinel: clean shutdown.")
}
