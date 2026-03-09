# MCP Sentinel CE

MCP Sentinel CE is a transparent local proxy for MCP stdio servers.

It sits between an MCP client and an MCP server, forwards JSON-RPC traffic with near-zero overhead, and blocks unsafe tool invocations using static regular expressions defined in `rules.json`.

## What CE does

- Proxies MCP traffic over `stdio`
- Intercepts inbound `tools/call` requests
- Blocks requests when:
  - the tool name matches `blocked_tools`
  - the serialized arguments match any regex in `blocked_paths`
- Returns a JSON-RPC policy violation error to the client when blocked
- Streams local live telemetry over SSE at `http://127.0.0.1:7438/events`

## What CE covers

Sentinel CE protects the **tool invocation boundary**.

- It inspects inbound `tools/call` requests before they reach the MCP server
- It can block:
  - specific tool names via `blocked_tools`
  - specific argument patterns via `blocked_paths`
- `blocked_paths` is matched against the serialized arguments of the request, not against file contents returned later by the server

When Sentinel CE is placed in front of `@modelcontextprotocol/server-filesystem`, the two layers complement each other:

- `server-filesystem` defines which directory tree is exposed
- Sentinel CE adds an extra deny layer on top of that exposed tree

Example:

- `server-filesystem` may expose `/Users/alice/project`
- Sentinel CE may still block requests containing patterns like `.env`, `.ssh`, `.pem`, `secret`, or `../`

This means CE can deny access to sensitive paths **inside** an otherwise allowed directory tree.

## What CE does not do

Community Edition intentionally does **not** include:

- outbound response inspection
- indirect prompt injection mitigation
- dynamic filesystem sandboxing
- `--allow-dir` / `--pass-allow-dir`
- authenticated Hub/Relay telemetry
- `/health` or `/emit` HTTP endpoints

In particular, CE does **not** inspect the content returned by a permitted tool call.

So if a readable file contains malicious text such as prompt injection instructions, CE will not redact or block that response. That response-inspection layer belongs to Enterprise Edition.

If another Sentinel process is already using `127.0.0.1:7438`, the current CE process still works as a proxy and continues handling MCP traffic normally, but it will not expose its own Live Monitor or SSE telemetry stream.

## Security model

Sentinel CE is intentionally simple and fail-secure.

- `os.Stdout` is reserved for MCP JSON-RPC traffic only
- logs and observability go to `os.Stderr`
- startup is quiet by default; set `SENTINEL_DEBUG=1` to enable verbose proxy logs on `stderr`
- if `rules.json` is missing or malformed, Sentinel refuses to start
- `rules.json` only accepts:
  - `blocked_tools`
  - `blocked_paths`
- unknown fields are rejected

## rules.json

Example:

```json
{
  "blocked_tools": ["execute_command"],
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
| `blocked_paths` | `string[]` | Go regular expressions matched against serialized tool arguments before they reach the MCP server |

## Usage

```bash
mcp-sentinel [--rules <path>] [--disable-telemetry] [--telemetry-hub-mode <relay|on_update|always_takeover>] <server-command> [server-args...]
```

Notes:

- `--rules` overrides the rules file path
- if `--rules` is omitted, Sentinel looks for `~/.sentinel/rules.json` and then `./rules.json`
- `--telemetry-hub-mode` is accepted for compatibility, but CE telemetry is local-only

## Example

```bash
./bin/mcp-sentinel --rules ./rules.json npx -y @modelcontextprotocol/server-filesystem /path/to/folder
```

### `server-filesystem` interaction

If you run Sentinel CE in front of `@modelcontextprotocol/server-filesystem`, remember:

- the filesystem server decides which root directory is exposed
- Sentinel CE only adds static deny rules on top of the incoming request arguments
- CE does **not** inspect or sanitize the text content returned from files that were allowed to be read

So CE is best understood as:

- a transparent local proxy
- a static request filter
- an extra deny layer over your MCP server

It is **not** a content-inspection firewall in Community Edition.

## User config

Optional user config lives at `~/.sentinel/config.json`.

Example:

```json
{
  "telemetry_enabled": true,
  "telemetry_hub_mode": "relay"
}
```

## Live Monitor

When telemetry is enabled and this process owns port `127.0.0.1:7438`, Sentinel serves:

- `/` — embedded Live Monitor HTML
- `/events` — Server-Sent Events stream

Open:

```bash
open http://127.0.0.1:7438
```

Or inspect the raw stream:

```bash
curl -N http://127.0.0.1:7438/events
```

## Build

```bash
go build ./...
```

## Test

```bash
go test ./...
```

## Architecture summary

```text
MCP Client ──stdio──> Sentinel CE ──stdio──> MCP Server
                  │
                  ├─ inbound policy check (`blocked_tools` + `blocked_paths`)
                  └─ optional local SSE telemetry (`/events`)
```

Sentinel CE is a local transparent proxy that blocks tools using static regular expressions in `rules.json`.
