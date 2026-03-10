# MCP Sentinel CE

<p align="center">
  <strong>A zero-trust local firewall for MCP stdio servers.</strong><br>
  Inspect inbound <code>tools/call</code> traffic, block unsafe tool invocations with static policy rules, and keep observability out of the JSON-RPC channel.
</p>

<p align="center">
  <a href="https://github.com/Aether-Labs-Studio/mcp-sentinel/releases"><img alt="Release" src="https://img.shields.io/github/v/release/Aether-Labs-Studio/mcp-sentinel"></a>
  <a href="/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
  <img alt="Go" src="https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go">
  <img alt="Platforms" src="https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey">
</p>

MCP Sentinel CE is a transparent proxy that sits between an MCP client and an MCP stdio server.

It forwards JSON-RPC traffic with very low overhead, inspects inbound `tools/call` requests, blocks requests that match your policy, and optionally streams local telemetry to a built-in Live Monitor.

> [!IMPORTANT]
> Community Edition protects the **tool invocation boundary**. It does **not** inspect or redact tool responses returned by the MCP server.

---

## Table of contents

- [Why MCP Sentinel CE](#why-mcp-sentinel-ce)
- [What CE does](#what-ce-does)
- [What CE does not do](#what-ce-does-not-do)
- [Installation](#installation)
  - [Install with Homebrew](#install-with-homebrew)
  - [Install with curl](#install-with-curl)
  - [Manual installation from releases](#manual-installation-from-releases)
  - [Build from source](#build-from-source)
- [Quick start](#quick-start)
- [CLI reference](#cli-reference)
- [Configuration files](#configuration-files)
- [rules.json reference](#rulesjson-reference)
- [Client integration](#client-integration)
- [Live Monitor and telemetry](#live-monitor-and-telemetry)
- [Security model](#security-model)
- [How CE works with server-filesystem](#how-ce-works-with-server-filesystem)
- [Architecture](#architecture)
- [Development](#development)
- [License](#license)

---

## Why MCP Sentinel CE

MCP clients are powerful because they can call tools. That is also where a lot of risk appears.

MCP Sentinel CE gives you a simple, explicit control point:

- block specific tools outright
- block requests whose serialized arguments match sensitive path patterns
- keep `stdout` reserved for MCP JSON-RPC only
- get a local stream of invocation/block events for debugging and auditing
- add a deny layer in front of existing MCP servers without modifying them

This makes CE especially useful when you want a lightweight hardening layer in front of stdio servers such as `@modelcontextprotocol/server-filesystem`.

---

## What CE does

- Proxies MCP traffic over `stdio`
- Intercepts inbound `tools/call` requests before they reach the server
- Blocks requests when:
  - the tool name matches `blocked_tools`
  - the serialized arguments match any regex in `blocked_paths`
- Returns a JSON-RPC policy violation error directly to the client
- Streams local live telemetry over SSE at `http://127.0.0.1:7438/events`
- Writes an append-only audit log to `~/.sentinel/audit.log` when telemetry is enabled and the local hub is active

### What gets inspected

Sentinel CE only inspects inbound JSON-RPC requests with:

- `method = "tools/call"`

Everything else is forwarded transparently.

---

## What CE does not do

Community Edition intentionally does **not** include:

- outbound response inspection
- indirect prompt injection mitigation
- dynamic filesystem sandboxing
- `--allow-dir`
- `--pass-allow-dir`
- authenticated Hub/Relay telemetry
- `/health` or `/emit` HTTP endpoints

In practical terms:

- if a tool call is **allowed**, CE does **not** inspect the text returned later by that tool
- if a readable file contains prompt injection text, CE will not redact it in Community Edition
- if another Sentinel instance already owns `127.0.0.1:7438`, this process still proxies traffic normally, but its local telemetry degrades to a silent no-op in this process

---

## Installation

### Install with Homebrew

The canonical Homebrew formula is published to the [Aether Labs tap](https://github.com/Aether-Labs-Studio/homebrew-tap).

```bash
brew install Aether-Labs-Studio/tap/mcp-sentinel
```

If you prefer tapping first:

```bash
brew tap Aether-Labs-Studio/tap
brew install mcp-sentinel
```

After installing the binary with Homebrew, you can run the installer in **configuration-only** mode to set up `~/.sentinel/` and register clients automatically:

```bash
curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | SKIP_BINARY=1 sh
```

The installer will ask which folder should be exposed by `@modelcontextprotocol/server-filesystem` before it tries to auto-configure any client. In CE, that value is stored in `~/.sentinel/config.json` as `default_filesystem_root` and reused across client configs.

### Install with curl

This downloads the latest release binary for your platform, installs it, creates `~/.sentinel/`, and offers to auto-configure supported MCP clients. The installer supports macOS and Linux. Windows users should use the manual release assets.

```bash
curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | sh
```

Installer environment variables:

| Variable | Meaning |
|---|---|
| `VERSION=v1.0.0` | Install a specific release instead of the latest |
| `INSTALL_DIR=/custom/bin` | Override the binary install directory |
| `SENTINEL_DIR=/custom/sentinel` | Override the Sentinel config directory |
| `FILESYSTEM_ROOT=/path/to/folder` | Root path stored in `~/.sentinel/config.json` as `default_filesystem_root` |
| `MCP_CLIENTS=gemini,cursor,codex` | Non-interactive list of clients to auto-configure (`all` or `none` also supported) |
| `YES=1` | Skip interactive confirmations |
| `SKIP_BINARY=1` | Skip binary download and only configure clients / files |

Examples:

```bash
curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | VERSION=v1.0.0 sh
```

```bash
curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | INSTALL_DIR="$HOME/.local/bin" YES=1 sh
```

```bash
curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | FILESYSTEM_ROOT="$HOME/project" YES=1 sh
```

```bash
curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | FILESYSTEM_ROOT="$HOME/project" MCP_CLIENTS=gemini,cursor YES=1 sh
```

> [!TIP]
> If you leave the filesystem root blank, the installer skips client auto-configuration and prints manual config snippets instead.

### Manual installation from releases

Download the binary for your platform from [GitHub Releases](https://github.com/Aether-Labs-Studio/mcp-sentinel/releases), make it executable, and place it on your `PATH`.

Release assets follow this naming pattern:

- `mcp-sentinel-vX.Y.Z-darwin-amd64`
- `mcp-sentinel-vX.Y.Z-darwin-arm64`
- `mcp-sentinel-vX.Y.Z-linux-amd64`
- `mcp-sentinel-vX.Y.Z-linux-arm64`
- `mcp-sentinel-vX.Y.Z-windows-amd64.exe`
- `checksums.txt`
- `rules.json`
- `config.json`

After a manual install, create `~/.sentinel/` and copy `rules.json` there, or pass an explicit rules file path with `--rules`.

### Build from source

Requirements:

- Go 1.25+

```bash
git clone https://github.com/Aether-Labs-Studio/mcp-sentinel.git
cd mcp-sentinel
make build
```

Output:

```text
bin/mcp-sentinel
```

Cross-platform builds:

```bash
make build-all
```

---

## Quick start

### Fastest path

1. Install Sentinel.
2. Edit `~/.sentinel/rules.json`.
3. Start your MCP server through Sentinel instead of launching it directly.

Example with `server-filesystem`:

```bash
mcp-sentinel npx -y @modelcontextprotocol/server-filesystem /path/to/folder
```

Example with an explicit rules file:

```bash
mcp-sentinel --rules ./rules.json npx -y @modelcontextprotocol/server-filesystem /path/to/folder
```

### What happens when a request is blocked

If a request violates policy, Sentinel does **not** forward it to the MCP server. Instead, it returns a JSON-RPC error to the client:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32600,
    "message": "Invalid Request - Policy Violation"
  }
}
```

---

## CLI reference

### Usage

```bash
mcp-sentinel [--rules <path>] [--disable-telemetry] [--telemetry-hub-mode <relay|on_update|always_takeover>] <server-command> [server-args...]
```

### Flags

| Flag | Meaning | Notes |
|---|---|---|
| `--rules <path>` | Use an explicit `rules.json` path | If omitted, Sentinel looks for `~/.sentinel/rules.json` and then `./rules.json` |
| `--disable-telemetry` | Disable local telemetry and SSE streaming | No local Live Monitor, no local audit stream |
| `--telemetry-hub-mode <relay|on_update|always_takeover>` | Accepted for compatibility | CE validates the value, but local CE behavior remains local-only |

### Rules file resolution order

If `--rules` is not provided, Sentinel resolves the rules file in this order:

1. `~/.sentinel/rules.json`
2. `./rules.json`

If no valid rules file is found, Sentinel refuses to start.

### Environment variables

| Variable | Meaning |
|---|---|
| `SENTINEL_DEBUG=1` | Enables verbose proxy logs on `stderr` |

Example:

```bash
SENTINEL_DEBUG=1 mcp-sentinel --rules ~/.sentinel/rules.json npx -y @modelcontextprotocol/server-filesystem ~/project
```

---

## Configuration files

### `~/.sentinel/rules.json`

This file defines the blocking policy.

### `~/.sentinel/config.json`

Optional user defaults:

```json
{
  "default_filesystem_root": "/Users/you/your-project",
  "telemetry_enabled": true,
  "telemetry_hub_mode": "relay"
}
```

Supported fields:

| Field | Type | Meaning |
|---|---:|---|
| `default_filesystem_root` | `string` | Default directory appended to `@modelcontextprotocol/server-filesystem` when the client config omits an explicit root |
| `telemetry_enabled` | `boolean` | Disable telemetry by default when set to `false` |
| `telemetry_hub_mode` | `string` | Optional compatibility value: `relay`, `on_update`, `always_takeover` |

### Precedence rules

- explicit CLI flags win over `~/.sentinel/config.json`
- `--disable-telemetry` overrides `telemetry_enabled`
- `--telemetry-hub-mode` overrides `telemetry_hub_mode`

---

## rules.json reference

Default structure:

```json
{
  "blocked_tools": [],
  "blocked_paths": [
    "(?i)/etc/passwd",
    "(?i)\\.ssh",
    "\\.\\.[/\\\\]"
  ]
}
```

### Schema

| Field | Type | Meaning |
|---|---:|---|
| `blocked_tools` | `string[]` | Tool names blocked unconditionally |
| `blocked_paths` | `string[]` | Go regular expressions matched against serialized tool arguments |

### Important behavior

- unknown fields are rejected
- invalid regex patterns fail startup
- matching happens against the **serialized arguments payload**, not against file contents returned later by the server

### Example: block a dangerous tool completely

```json
{
  "blocked_tools": ["execute_command"],
  "blocked_paths": []
}
```

### Example: harden a filesystem server

```json
{
  "blocked_tools": [],
  "blocked_paths": [
    "(?i)\\.env",
    "(?i)\\.ssh",
    "(?i)id_rsa",
    "(?i)secret",
    "\\.\\.[/\\\\]"
  ]
}
```

---

## Client integration

The installer can auto-detect and offer to configure these clients when present:

- Claude Code
- Gemini CLI
- Cursor
- VS Code
- Antigravity
- OpenCode
- Codex

Before auto-configuring any client, the installer asks for the folder to expose through `@modelcontextprotocol/server-filesystem`.
That folder is stored once in `~/.sentinel/config.json` as `default_filesystem_root`, and Sentinel appends it automatically when launching `server-filesystem` without an explicit root argument.

Then:

- in interactive terminals, the installer shows one list of detected clients
- nothing is preselected by default
- you can toggle clients by entering numbers like `1 3`
- press Enter on an empty line to confirm the current selection
- `all` selects every detected client and `none` clears the selection
- in non-interactive flows, set `MCP_CLIENTS=...`
- if you leave the filesystem root blank, Sentinel is still installed but the installer falls back to manual snippets

### Generic MCP config snippet

For clients that support stdio MCP servers through a JSON config, the core shape is:

```json
{
  "mcpServers": {
    "sentinel": {
      "command": "mcp-sentinel",
      "args": ["npx", "-y", "@modelcontextprotocol/server-filesystem"]
    }
  }
}
```

### Manual configuration by client

#### Claude Code

```bash
claude mcp add --scope user sentinel -- mcp-sentinel \
  npx -y @modelcontextprotocol/server-filesystem
```

#### Gemini CLI

`~/.gemini/settings.json`

```json
{
  "mcpServers": {
    "sentinel": {
      "type": "stdio",
      "command": "mcp-sentinel",
      "args": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
      "env": {}
    }
  }
}
```

#### Cursor

`~/.cursor/mcp.json`

```json
{
  "mcpServers": {
    "sentinel": {
      "type": "stdio",
      "command": "mcp-sentinel",
      "args": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
      "env": {}
    }
  }
}
```

#### VS Code

`~/.vscode/mcp.json`

```json
{
  "servers": {
    "sentinel": {
      "command": "mcp-sentinel",
      "args": ["npx", "-y", "@modelcontextprotocol/server-filesystem"]
    }
  }
}
```

#### Antigravity

`~/.gemini/antigravity/mcp_config.json`

```json
{
  "mcpServers": {
    "sentinel": {
      "type": "stdio",
      "command": "mcp-sentinel",
      "args": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
      "env": {}
    }
  }
}
```

#### OpenCode

`~/.config/opencode/opencode.json`

```json
{
  "mcp": {
    "sentinel": {
      "type": "local",
      "command": ["mcp-sentinel", "npx", "-y", "@modelcontextprotocol/server-filesystem"],
      "enabled": true
    }
  }
}
```

#### Codex

`~/.codex/config.toml`

```toml
[mcp_servers.sentinel]
command = "mcp-sentinel"
args = ["npx", "-y", "@modelcontextprotocol/server-filesystem"]
```

### Why Sentinel appears as the command

The MCP client should launch **Sentinel**, and Sentinel should launch the real MCP server as its subprocess.

That is how Sentinel stays on the transport path and can inspect inbound traffic before the server receives it.

---

## Live Monitor and telemetry

When telemetry is enabled and this process owns `127.0.0.1:7438`, Sentinel serves:

- `/` — embedded Live Monitor HTML
- `/events` — Server-Sent Events stream

Open the monitor:

```bash
open http://127.0.0.1:7438
```

Inspect the raw event stream:

```bash
curl -N http://127.0.0.1:7438/events
```

Telemetry emits events like:

- `INVOCATION` — a clean `tools/call` request passed policy
- `BLOCK` — a `tools/call` request was rejected

Notes:

- telemetry is local-only in CE
- if the telemetry port is already in use, Sentinel keeps proxying traffic and silently skips creating a new local hub in that process
- audit events are also written to `~/.sentinel/audit.log` when the local hub is active

---

## Security model

Sentinel CE is intentionally simple and fail-secure.

- `os.Stdout` is reserved for MCP JSON-RPC traffic only
- logs and observability go to `os.Stderr`
- startup is quiet by default
- if `rules.json` is missing or malformed, Sentinel refuses to start
- only `blocked_tools` and `blocked_paths` are accepted in `rules.json`
- unknown fields in `rules.json` are rejected
- malformed `tools/call.params` are left for the downstream server to handle

### Path resolution hardening

When Sentinel launches the child command, it first tries normal `PATH` lookup.
If that fails, it falls back to a login shell lookup so MCP clients on macOS can still resolve tools installed through shell-managed environments such as `nvm`, `rbenv`, or `pyenv`.

---

## How CE works with server-filesystem

When Sentinel CE is placed in front of `@modelcontextprotocol/server-filesystem`, the two layers complement each other:

- `server-filesystem` decides which directory tree is exposed
- Sentinel CE adds a static deny layer on top of incoming request arguments

Example:

- `server-filesystem` may expose `/Users/alice/project`
- Sentinel CE may still block requests containing patterns like `.env`, `.ssh`, `.pem`, `secret`, or `../`

That means CE can deny access to sensitive paths **inside** an otherwise allowed directory tree.

What it does **not** mean:

- CE does not inspect file contents returned by an allowed read
- CE does not sanitize the response payload in Community Edition

---

## Architecture

```text
MCP Client ──stdio──> Sentinel CE ──stdio──> MCP Server
                  │
                  ├─ inbound policy check (`blocked_tools` + `blocked_paths`)
                  ├─ optional local SSE telemetry (`/events`)
                  └─ optional local audit log (`~/.sentinel/audit.log`)
```

Sentinel CE is a local transparent proxy that blocks unsafe tool invocations using static regular expressions in `rules.json`.

---

## Development

Build:

```bash
make build
```

Run tests:

```bash
make test
```

Run vet:

```bash
make vet
```

Cross-build release artifacts:

```bash
make build-all
```

---

## License

MIT. See [/LICENSE](/LICENSE).
