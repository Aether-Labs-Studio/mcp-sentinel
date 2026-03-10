#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
#  MCP Sentinel — Installer
#  https://github.com/Aether-Labs-Studio/mcp-sentinel
#
#  Supported platforms: macOS (darwin/amd64, darwin/arm64), Linux (linux/amd64, linux/arm64)
#  Windows: not supported by this script — see README for manual installation instructions.
#
#  Usage:
#    curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | sh
#
#  Options (env vars):
#    VERSION=v1.0.0   Install a specific version (default: latest)
#    INSTALL_DIR=...  Override install directory (default: /usr/local/bin)
#    FILESYSTEM_ROOT=...
#                     Root path stored in ~/.sentinel/config.json as default_filesystem_root
#    YES=1            Skip all confirmation prompts (non-interactive / CI)
#    SKIP_BINARY=1    Skip binary download (e.g. already installed via Homebrew)
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

REPO="Aether-Labs-Studio/mcp-sentinel"
BINARY="mcp-sentinel"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SENTINEL_DIR="${SENTINEL_DIR:-$HOME/.sentinel}"
FILESYSTEM_ROOT="${FILESYSTEM_ROOT:-}"
MCP_CLIENTS="${MCP_CLIENTS:-}"
YES="${YES:-0}"
SKIP_BINARY="${SKIP_BINARY:-0}"
tmp_dir=""

# ── Colors ────────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'
  BLUE='\033[0;34m'; BOLD='\033[1m'; NC='\033[0m'
else
  GREEN=''; YELLOW=''; RED=''; BLUE=''; BOLD=''; NC=''
fi

info()    { printf "  ${BLUE}→${NC} %s\n" "$*"; }
success() { printf "  ${GREEN}✓${NC} %s\n" "$*"; }
warn()    { printf "  ${YELLOW}!${NC} %s\n" "$*" >&2; }
fatal()   { printf "  ${RED}✗${NC} %s\n" "$*" >&2; exit 1; }

# ── Platform detection ────────────────────────────────────────────────────────
detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$os" in
    linux)  OS="linux"  ;;
    darwin) OS="darwin" ;;
    *)      fatal "Unsupported OS: $os. Install manually from https://github.com/$REPO/releases" ;;
  esac

  case "$arch" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)             fatal "Unsupported architecture: $arch. Install manually from https://github.com/$REPO/releases" ;;
  esac

  PLATFORM="${OS}-${ARCH}"
}

# ── Downloader ────────────────────────────────────────────────────────────────
download() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    fatal "curl or wget is required to download files."
  fi
}

# ── Latest version from GitHub API ───────────────────────────────────────────
get_version() {
  if [ -n "${VERSION:-}" ]; then
    echo "$VERSION"; return
  fi

  local api_url="https://api.github.com/repos/$REPO/releases/latest"
  local v

  if command -v curl >/dev/null 2>&1; then
    v=$(curl -fsSL "$api_url" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  else
    v=$(wget -qO- "$api_url" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  fi

  [ -z "$v" ] && fatal "Could not determine latest version. Set VERSION=v1.0.0 to override."
  echo "$v"
}

# ── Checksum verification ─────────────────────────────────────────────────────
verify_checksum() {
  local file="$1" checksums_file="$2" filename="$3"
  local expected actual

  expected=$(grep "$filename" "$checksums_file" | awk '{print $1}')
  [ -z "$expected" ] && fatal "Checksum not found for $filename"

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$file" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$file" | awk '{print $1}')
  else
    warn "sha256sum/shasum not found — skipping checksum verification."
    return 0
  fi

  [ "$actual" = "$expected" ] || fatal "Checksum mismatch for $filename"
  success "Checksum verified"
}

# ── Interactive confirmation ──────────────────────────────────────────────────
confirm() {
  local msg="$1"
  [ "$YES" = "1" ] && return 0
  if [ -e /dev/tty ]; then
    printf "  %s [y/N] " "$msg" >/dev/tty
    read -r REPLY </dev/tty
  else
    return 1  # non-interactive pipe — skip optional steps
  fi
  case "$REPLY" in [yY]*) return 0 ;; *) return 1 ;; esac
}

# ── JSON patcher (Python 3) ───────────────────────────────────────────────────
# Merges an mcpServers entry into an existing or new JSON config file.
# Exit codes: 0 = written, 1 = error, 2 = exists with correct path (skip),
#             3 = exists with different binary path (needs update).
patch_json_mcp() {
  local file="$1" key="$2" value="$3"

  command -v python3 >/dev/null 2>&1 || return 1

  python3 - "$file" "$key" "$value" <<'PYEOF'
import sys, json, os

path, key, value_str = sys.argv[1], sys.argv[2], sys.argv[3]
value = json.loads(value_str)

if os.path.exists(path):
    with open(path) as f:
        try:
            data = json.load(f)
        except json.JSONDecodeError:
            print(f"  Warning: {path} contains invalid JSON — skipping.", file=sys.stderr)
            sys.exit(1)
else:
    data = {}

existing = data.get("mcpServers", {}).get(key)
if existing is not None:
    if existing.get("command") == value.get("command"):
        sys.exit(2)  # already configured with correct path
    else:
        sys.exit(3)  # configured with a different binary path

data.setdefault("mcpServers", {})[key] = value

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PYEOF
}

# Overwrites an existing mcpServers entry unconditionally.
patch_json_mcp_update() {
  local file="$1" key="$2" value="$3"

  command -v python3 >/dev/null 2>&1 || return 1

  python3 - "$file" "$key" "$value" <<'PYEOF'
import sys, json, os

path, key, value_str = sys.argv[1], sys.argv[2], sys.argv[3]
value = json.loads(value_str)

if os.path.exists(path):
    with open(path) as f:
        try:
            data = json.load(f)
        except json.JSONDecodeError:
            print(f"  Warning: {path} contains invalid JSON — skipping.", file=sys.stderr)
            sys.exit(1)
else:
    data = {}

data.setdefault("mcpServers", {})[key] = value

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PYEOF
}

# ── Config snippets ───────────────────────────────────────────────────────────
print_claude_snippet() {
  local bin="$1" root="${2:-/path/to/folder}"
  local server_args='npx -y @modelcontextprotocol/server-filesystem'
  if [ "$root" = "/path/to/folder" ]; then
    server_args="$server_args $root"
  fi
  printf "\n    Run this to register Sentinel in Claude Code:\n\n"
  printf "    ${BOLD}claude mcp add --scope user sentinel -- %s \\\\\n" "$bin"
  printf "      %s${NC}\n\n" "$server_args"
}

print_generic_snippet() {
  local bin="$1" root="${2:-/path/to/folder}"
  local args='["npx", "-y", "@modelcontextprotocol/server-filesystem"]'
  if [ "$root" = "/path/to/folder" ]; then
    args='["npx", "-y", "@modelcontextprotocol/server-filesystem", "/path/to/folder"]'
  fi
  cat <<EOF

    {
      "mcpServers": {
        "sentinel": {
          "command": "$bin",
          "args": $args
        }
      }
    }

EOF
}

print_vscode_snippet() {
  local bin="$1" root="${2:-/path/to/folder}"
  local args='["npx", "-y", "@modelcontextprotocol/server-filesystem"]'
  if [ "$root" = "/path/to/folder" ]; then
    args='["npx", "-y", "@modelcontextprotocol/server-filesystem", "/path/to/folder"]'
  fi
  cat <<EOF

    {
      "servers": {
        "sentinel": {
          "command": "$bin",
          "args": $args
        }
      }
    }

EOF
}

print_opencode_snippet() {
  local bin="$1" root="${2:-/path/to/folder}"
  local cmd="[\"$bin\", \"npx\", \"-y\", \"@modelcontextprotocol/server-filesystem\"]"
  if [ "$root" = "/path/to/folder" ]; then
    cmd="[\"$bin\", \"npx\", \"-y\", \"@modelcontextprotocol/server-filesystem\", \"/path/to/folder\"]"
  fi
  cat <<EOF

    {
      "mcp": {
        "sentinel": {
          "type": "local",
          "command": $cmd,
          "enabled": true
        }
      }
    }

EOF
}

print_codex_snippet() {
  local bin="$1" root="${2:-/path/to/folder}"
  local args='["npx", "-y", "@modelcontextprotocol/server-filesystem"]'
  if [ "$root" = "/path/to/folder" ]; then
    args='["npx", "-y", "@modelcontextprotocol/server-filesystem", "/path/to/folder"]'
  fi
  cat <<EOF

    [mcp_servers.sentinel]
    command = "$bin"
    args = $args

EOF
}

prompt_filesystem_root() {
  if [ -n "$FILESYSTEM_ROOT" ]; then
    info "Filesystem root: $FILESYSTEM_ROOT"
    return 0
  fi

  if [ -e /dev/tty ]; then
    printf "  Enter the folder to expose via @modelcontextprotocol/server-filesystem\n" >/dev/tty
    printf "  Sentinel CE will store it in ~/.sentinel/config.json as default_filesystem_root.\n" >/dev/tty
    printf "  Leave blank to skip auto-config and get manual config examples instead.\n\n" >/dev/tty
    printf "  Filesystem root: " >/dev/tty
    read -r FILESYSTEM_ROOT </dev/tty || FILESYSTEM_ROOT=""
    if [ -n "$FILESYSTEM_ROOT" ]; then
      success "Filesystem root set → $FILESYSTEM_ROOT"
    else
      info "No filesystem root provided — auto-config will be skipped"
    fi
  else
    info "No TTY available and FILESYSTEM_ROOT not set — auto-config will be skipped"
  fi
}

update_sentinel_config() {
  [ -z "$FILESYSTEM_ROOT" ] && return 0

  local config_path="$SENTINEL_DIR/config.json"
  mkdir -p "$SENTINEL_DIR"

  command -v python3 >/dev/null 2>&1 || return 1

  python3 - "$config_path" "$FILESYSTEM_ROOT" <<'PYEOF'
import sys, json, os
path, default_filesystem_root = sys.argv[1], sys.argv[2]

if os.path.exists(path):
    with open(path) as f:
        try:
            data = json.load(f)
        except json.JSONDecodeError:
            print(f"  Warning: {path} contains invalid JSON — skipping.", file=sys.stderr)
            sys.exit(1)
else:
    data = {"telemetry_enabled": True, "telemetry_hub_mode": "relay"}

data["default_filesystem_root"] = default_filesystem_root

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PYEOF
}

ensure_sentinel_defaults() {
  mkdir -p "$SENTINEL_DIR"

  if [ ! -f "$SENTINEL_DIR/rules.json" ]; then
    if [ -f "./rules.json" ]; then
      cp "./rules.json" "$SENTINEL_DIR/rules.json"
      success "Rules installed → $SENTINEL_DIR/rules.json"
    else
      local base_url="https://github.com/$REPO/releases/download/$(get_version)"
      info "Downloading rules.json..."
      download "${base_url}/rules.json" "$SENTINEL_DIR/rules.json"
      success "Rules installed → $SENTINEL_DIR/rules.json"
    fi
  else
    info "rules.json already exists — skipping (your customizations are safe)"
  fi

  if [ ! -f "$SENTINEL_DIR/config.json" ]; then
    if [ -f "./config.json" ]; then
      cp "./config.json" "$SENTINEL_DIR/config.json"
      success "Config installed → $SENTINEL_DIR/config.json"
    else
      local base_url="https://github.com/$REPO/releases/download/$(get_version)"
      info "Downloading config.json..."
      download "${base_url}/config.json" "$SENTINEL_DIR/config.json"
      success "Config installed → $SENTINEL_DIR/config.json"
    fi
  fi

  if update_sentinel_config; then
    success "Config updated → $SENTINEL_DIR/config.json (default_filesystem_root)"
  else
    warn "Could not update $SENTINEL_DIR/config.json with default_filesystem_root"
  fi
}

print_manual_client_examples() {
  local bin="$1"
  local root="${2:-/path/to/folder}"

  printf "\n  Add Sentinel manually with your real folder path in place of ${BOLD}%s${NC}:\n" "$root"

  printf "\n  ${BOLD}Claude Code${NC}\n"
  print_claude_snippet "$bin" "$root"

  printf "  ${BOLD}Gemini CLI / Cursor / generic mcpServers JSON${NC}\n"
  print_generic_snippet "$bin" "$root"

  printf "  ${BOLD}VS Code${NC}\n"
  print_vscode_snippet "$bin" "$root"

  printf "  ${BOLD}Antigravity${NC}\n"
  print_generic_snippet "$bin" "$root"

  printf "  ${BOLD}OpenCode${NC}\n"
  print_opencode_snippet "$bin" "$root"

  printf "  ${BOLD}Codex${NC}\n"
  print_codex_snippet "$bin" "$root"
}

normalize_client_id() {
  echo "$1" | tr '[:upper:]' '[:lower:]' | tr -d ' _-'
}

client_label() {
  case "$1" in
    claude) echo "Claude Code" ;;
    gemini) echo "Gemini CLI" ;;
    cursor) echo "Cursor" ;;
    vscode) echo "VS Code" ;;
    antigravity) echo "Antigravity" ;;
    opencode) echo "OpenCode" ;;
    codex) echo "Codex" ;;
    *) echo "$1" ;;
  esac
}

client_path() {
  case "$1" in
    claude) echo "~/.claude.json" ;;
    gemini) echo "~/.gemini/settings.json" ;;
    cursor) echo "~/.cursor/mcp.json" ;;
    vscode) echo "~/.vscode/mcp.json" ;;
    antigravity) echo "~/.gemini/antigravity/mcp_config.json" ;;
    opencode) echo "~/.config/opencode/opencode.json" ;;
    codex) echo "~/.codex/config.toml" ;;
    *) echo "" ;;
  esac
}

client_toggle_selected() {
  local id="$1"
  local kept=""
  local found=0
  for existing in ${SELECTED_CLIENT_IDS:-}; do
    if [ "$existing" = "$id" ]; then
      found=1
      continue
    fi
    kept="${kept}${kept:+ }$existing"
  done
  if [ "$found" -eq 0 ]; then
    kept="${kept}${kept:+ }$id"
  fi
  SELECTED_CLIENT_IDS="$kept"
}

should_configure_client() {
  local id="$1"
  for selected in ${SELECTED_CLIENT_IDS:-}; do
    [ "$selected" = "$id" ] && return 0
  done
  return 1
}

print_client_selection_menu() {
  local idx id
  printf "\n  ${BOLD}Detected MCP clients${NC}\n"
  printf "  Mark one or more clients to auto-configure.\n"
  printf "  Type numbers separated by spaces (or 'all' / 'none'), then press Enter.\n\n"
  idx=1
  for id in ${DETECTED_CLIENT_IDS:-}; do
    local mark=" "
    if should_configure_client "$id"; then
      mark="x"
    fi
    printf "  [%s] %d) %s (%s)\n" "$mark" "$idx" "$(client_label "$id")" "$(client_path "$id")"
    idx=$((idx + 1))
  done
  printf "      a) Select all detected clients\n"
  printf "      n) Clear selection (manual setup)\n"
}

select_clients_interactively() {
  local tokens token normalized idx id selection

  SELECTED_CLIENT_IDS=""

  while true; do
    print_client_selection_menu >/dev/tty
    printf "\n  Selection: " >/dev/tty
    read -r selection </dev/tty || selection=""
    [ -z "$selection" ] && break

    tokens=$(echo "$selection" | tr ',' ' ')
    for token in $tokens; do
      normalized=$(normalize_client_id "$token")
      case "$normalized" in
        a|all)
          SELECTED_CLIENT_IDS="$DETECTED_CLIENT_IDS"
          continue
          ;;
        n|none|skip|manual)
          SELECTED_CLIENT_IDS=""
          continue
          ;;
      esac

      if [ "$normalized" -ge 1 ] 2>/dev/null; then
        idx=1
        for id in $DETECTED_CLIENT_IDS; do
          if [ "$idx" = "$normalized" ]; then
            client_toggle_selected "$id"
            break
          fi
          idx=$((idx + 1))
        done
        continue
      fi

      for id in $DETECTED_CLIENT_IDS; do
        if [ "$(normalize_client_id "$id")" = "$normalized" ]; then
          client_toggle_selected "$id"
          break
        fi
      done
    done
  done
}

select_clients_from_env() {
  local requested token normalized id picked=""
  requested=$(echo "$MCP_CLIENTS" | tr ',' ' ')

  for token in $requested; do
    normalized=$(normalize_client_id "$token")
    case "$normalized" in
      a|all)
        SELECTED_CLIENT_IDS="$DETECTED_CLIENT_IDS"
        return 0
        ;;
      n|none|skip|manual)
        SELECTED_CLIENT_IDS=""
        return 0
        ;;
    esac

    for id in $DETECTED_CLIENT_IDS; do
      if [ "$(normalize_client_id "$id")" = "$normalized" ]; then
        picked="${picked}${picked:+ }$id"
        break
      fi
    done
  done

  SELECTED_CLIENT_IDS="$picked"
}

select_clients() {
  if [ -n "$MCP_CLIENTS" ]; then
    info "Client selection from MCP_CLIENTS=$MCP_CLIENTS"
    select_clients_from_env
    return 0
  fi

  if [ -e /dev/tty ]; then
    select_clients_interactively
    return 0
  fi

  info "No TTY available and MCP_CLIENTS not set — client auto-config will be skipped"
  SELECTED_CLIENT_IDS=""
}

print_selected_clients_summary() {
  printf "\n  ${BOLD}Configuration summary${NC}\n"
  printf "  Filesystem root: %s\n" "$FILESYSTEM_ROOT"
  if [ -n "${SELECTED_CLIENT_IDS:-}" ]; then
    printf "  Clients selected:\n"
    for id in $SELECTED_CLIENT_IDS; do
      printf "    - %s (%s)\n" "$(client_label "$id")" "$(client_path "$id")"
    done
  else
    printf "  Clients selected: none (manual setup)\n"
  fi
}

# ── MCP client auto-configuration ────────────────────────────────────────────

# Handles the result of a patch operation, including the path-mismatch case.
# Usage: handle_mcp_rc <rc> <client_name> <file> <entry> <fallback_fn>
handle_mcp_rc() {
  local rc="$1" name="$2" file="$3" entry="$4" fallback="$5"
  if [ "$rc" -eq 0 ]; then
    success "$name configured → $file"
    configured=$((configured + 1))
  elif [ "$rc" -eq 2 ]; then
    info "$name already configured — skipping"
    configured=$((configured + 1))
  elif [ "$rc" -eq 3 ]; then
    warn "$name is configured with a different binary path."
    if confirm "Update $name to use $binary_path?"; then
      local rc2=0; patch_json_mcp_update "$file" "sentinel" "$entry" || rc2=$?
      if [ "$rc2" -eq 0 ]; then
        success "$name path updated → $file"
        configured=$((configured + 1))
      else
        warn "Could not update $name."
      fi
    else
      info "$name skipped — keeping existing configuration"
      configured=$((configured + 1))
    fi
  else
    warn "Could not auto-configure $name."
    $fallback "$binary_path" "${FILESYSTEM_ROOT:-/path/to/folder}"
  fi
}

configure_clients() {
  # Resolve binary path.
  # In SKIP_BINARY mode the binary was not just installed here, so go straight
  # to PATH resolution (e.g. Homebrew symlink at /opt/homebrew/bin).
  # In normal mode prefer the just-installed path.
  local binary_path
  if [ "${SKIP_BINARY:-0}" = "1" ]; then
    binary_path=$(command -v "$BINARY" 2>/dev/null || echo "$INSTALL_DIR/$BINARY")
  elif [ -x "$INSTALL_DIR/$BINARY" ]; then
    binary_path="$INSTALL_DIR/$BINARY"
  elif command -v "$BINARY" >/dev/null 2>&1; then
    binary_path=$(command -v "$BINARY")
  else
    binary_path="$INSTALL_DIR/$BINARY"
  fi
  configured=0

  prompt_filesystem_root
  ensure_sentinel_defaults

  local sentinel_args
  sentinel_args=$(printf '["npx","-y","@modelcontextprotocol/server-filesystem"]')
  local sentinel_entry
  sentinel_entry=$(printf '{"type":"stdio","command":"%s","args":%s,"env":{}}' "$binary_path" "$sentinel_args")
  local vscode_entry
  vscode_entry=$(printf '{"command":"%s","args":%s}' "$binary_path" "$sentinel_args")
  local opencode_cmd
  opencode_cmd=$(printf '["%s","npx","-y","@modelcontextprotocol/server-filesystem"]' "$binary_path")
  local opencode_entry
  opencode_entry=$(printf '{"type":"local","command":%s,"enabled":true}' "$opencode_cmd")

  if [ -z "$FILESYSTEM_ROOT" ]; then
    print_manual_client_examples "$binary_path" "$FILESYSTEM_ROOT"
    return 0
  fi

  printf "\n  ${BOLD}Detecting MCP clients...${NC}\n"

  DETECTED_CLIENT_IDS=""

  # ── Claude Code ──────────────────────────────────────────────────────────
  local claude_json="$HOME/.claude.json"
  if [ -f "$claude_json" ] || command -v claude >/dev/null 2>&1; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }claude"
  fi

  # ── Gemini CLI ───────────────────────────────────────────────────────────
  local gemini_settings="$HOME/.gemini/settings.json"
  if [ -f "$gemini_settings" ] || [ -d "$HOME/.gemini" ]; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }gemini"
  fi

  # ── Cursor ───────────────────────────────────────────────────────────────
  local cursor_mcp="$HOME/.cursor/mcp.json"
  if [ -f "$cursor_mcp" ] || [ -d "$HOME/.cursor" ]; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }cursor"
  fi

  # ── VS Code ──────────────────────────────────────────────────────────────
  local vscode_mcp="$HOME/.vscode/mcp.json"
  if [ -f "$vscode_mcp" ] || command -v code >/dev/null 2>&1; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }vscode"
  fi

  # ── Antigravity ──────────────────────────────────────────────────────────
  local antigravity_mcp="$HOME/.gemini/antigravity/mcp_config.json"
  if [ -d "$HOME/.gemini/antigravity" ] || [ -f "$antigravity_mcp" ]; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }antigravity"
  fi

  # ── OpenCode ─────────────────────────────────────────────────────────────
  local opencode_config="$HOME/.config/opencode/opencode.json"
  if [ -f "$opencode_config" ] || command -v opencode >/dev/null 2>&1; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }opencode"
  fi

  # ── Codex ────────────────────────────────────────────────────────────────
  local codex_config="$HOME/.codex/config.toml"
  if [ -f "$codex_config" ] || command -v codex >/dev/null 2>&1; then
    DETECTED_CLIENT_IDS="${DETECTED_CLIENT_IDS}${DETECTED_CLIENT_IDS:+ }codex"
  fi

  if [ -z "$DETECTED_CLIENT_IDS" ]; then
    printf "\n  No supported MCP clients detected for auto-configuration.\n"
    print_manual_client_examples "$binary_path" "$FILESYSTEM_ROOT"
    return 0
  fi

  select_clients
  print_selected_clients_summary

  if [ -n "$SELECTED_CLIENT_IDS" ] && ! confirm "Proceed with the selected client updates?"; then
    info "Client auto-configuration skipped"
    SELECTED_CLIENT_IDS=""
  fi

  if [ -z "$SELECTED_CLIENT_IDS" ]; then
    print_manual_client_examples "$binary_path" "$FILESYSTEM_ROOT"
    return 0
  fi

  if should_configure_client "claude"; then
    printf "\n  ${BOLD}Claude Code${NC}\n"
      rc=0; patch_json_mcp "$claude_json" "sentinel" "$sentinel_entry" || rc=$?
      handle_mcp_rc "$rc" "Claude Code" "$claude_json" "$sentinel_entry" "print_claude_snippet"
  fi

  if should_configure_client "gemini"; then
    printf "\n  ${BOLD}Gemini CLI${NC}\n"
    mkdir -p "$HOME/.gemini"
    rc=0; patch_json_mcp "$gemini_settings" "sentinel" "$sentinel_entry" || rc=$?
    handle_mcp_rc "$rc" "Gemini CLI" "$gemini_settings" "$sentinel_entry" "print_generic_snippet"
  fi

  if should_configure_client "cursor"; then
    printf "\n  ${BOLD}Cursor${NC}\n"
    mkdir -p "$HOME/.cursor"
    rc=0; patch_json_mcp "$cursor_mcp" "sentinel" "$sentinel_entry" || rc=$?
    handle_mcp_rc "$rc" "Cursor" "$cursor_mcp" "$sentinel_entry" "print_generic_snippet"
  fi

  if should_configure_client "vscode"; then
    printf "\n  ${BOLD}VS Code${NC}\n"
    mkdir -p "$HOME/.vscode"
    rc=0; python3 - "$vscode_mcp" "sentinel" "$vscode_entry" <<'PYEOF' || rc=$?
import sys, json, os, re

def load_jsonc(path):
    """Load JSON that may contain trailing commas (JSONC format used by VS Code)."""
    with open(path) as f:
        content = f.read()
    content = re.sub(r',(\s*[}\]])', r'\1', content)
    return json.loads(content)

path, key, value_str = sys.argv[1], sys.argv[2], sys.argv[3]
value = json.loads(value_str)

try:
    data = load_jsonc(path) if os.path.exists(path) else {}
except (json.JSONDecodeError, OSError) as e:
    print(f"  Warning: could not parse {path}: {e}", file=sys.stderr)
    sys.exit(1)

# VS Code mcp.json may use "servers" or "mcpServers" — check both
existing = data.get("servers", {}).get(key) or data.get("mcpServers", {}).get(key)
if existing is not None:
    sys.exit(2 if existing.get("command") == value.get("command") else 3)

data.setdefault("servers", {})[key] = value
with open(path, "w") as f:
    json.dump(data, f, indent=2); f.write("\n")
PYEOF
    if [ "$rc" -eq 3 ]; then
      warn "VS Code is configured with a different binary path."
      if confirm "Update VS Code to use $binary_path?"; then
        python3 - "$vscode_mcp" "sentinel" "$vscode_entry" <<'PYEOF'
import sys, json, os, re

def load_jsonc(path):
    with open(path) as f:
        content = f.read()
    return json.loads(re.sub(r',(\s*[}\]])', r'\1', content))

path, key, value_str = sys.argv[1], sys.argv[2], sys.argv[3]
value = json.loads(value_str)
data = load_jsonc(path) if os.path.exists(path) else {}
# Update whichever key already exists
mcp_key = "servers" if "servers" in data else "mcpServers"
data.setdefault(mcp_key, {})[key] = value
with open(path, "w") as f:
    json.dump(data, f, indent=2); f.write("\n")
PYEOF
        success "VS Code path updated → $vscode_mcp"
        configured=$((configured + 1))
      else
        info "VS Code skipped — keeping existing configuration"
        configured=$((configured + 1))
      fi
    else
      handle_mcp_rc "$rc" "VS Code" "$vscode_mcp" "$vscode_entry" "print_generic_snippet"
    fi
  fi

  if should_configure_client "antigravity"; then
    printf "\n  ${BOLD}Antigravity${NC}\n"
    mkdir -p "$HOME/.gemini/antigravity"
    rc=0; patch_json_mcp "$antigravity_mcp" "sentinel" "$sentinel_entry" || rc=$?
    handle_mcp_rc "$rc" "Antigravity" "$antigravity_mcp" "$sentinel_entry" "print_generic_snippet"
  fi

  if should_configure_client "opencode"; then
    printf "\n  ${BOLD}OpenCode${NC}\n"
    mkdir -p "$HOME/.config/opencode"
    rc=0; python3 - "$opencode_config" "sentinel" "$opencode_entry" <<'PYEOF' 2>/dev/null || rc=$?
import sys, json, os
path, key, value_str = sys.argv[1], sys.argv[2], sys.argv[3]
value = json.loads(value_str)
if os.path.exists(path):
    with open(path) as f:
        try:
            data = json.load(f)
        except json.JSONDecodeError:
            print(f"  Warning: {path} contains invalid JSON — skipping.", file=sys.stderr)
            sys.exit(1)
else:
    data = {}
existing = data.get("mcp", {}).get(key)
if existing is not None:
    existing_bin = (existing.get("command") or [""])[0]
    new_bin = (value.get("command") or [""])[0]
    sys.exit(2 if existing_bin == new_bin else 3)
data.setdefault("mcp", {})[key] = value
with open(path, "w") as f:
    json.dump(data, f, indent=2); f.write("\n")
PYEOF
    if [ "$rc" -eq 3 ]; then
      warn "OpenCode is configured with a different binary path."
      if confirm "Update OpenCode to use $binary_path?"; then
        python3 - "$opencode_config" "sentinel" "$opencode_entry" <<'PYEOF' 2>/dev/null
import sys, json, os
path, key, value_str = sys.argv[1], sys.argv[2], sys.argv[3]
value = json.loads(value_str)
data = json.load(open(path)) if os.path.exists(path) else {}
data.setdefault("mcp", {})[key] = value
with open(path, "w") as f:
    json.dump(data, f, indent=2); f.write("\n")
PYEOF
        success "OpenCode path updated → $opencode_config"
        configured=$((configured + 1))
      else
        info "OpenCode skipped — keeping existing configuration"
        configured=$((configured + 1))
      fi
    else
      handle_mcp_rc "$rc" "OpenCode" "$opencode_config" "$opencode_entry" "print_generic_snippet"
    fi
  fi

  if should_configure_client "codex"; then
    printf "\n  ${BOLD}Codex${NC}\n"
    mkdir -p "$HOME/.codex"
    if ! grep -q '\[mcp_servers\.sentinel\]' "$codex_config" 2>/dev/null; then
      cat >> "$codex_config" <<TOML

[mcp_servers.sentinel]
command = "$binary_path"
args = ["npx", "-y", "@modelcontextprotocol/server-filesystem"]
TOML
      success "Codex configured → $codex_config"
      configured=$((configured + 1))
    elif ! grep -q "command = \"$binary_path\"" "$codex_config" 2>/dev/null; then
      warn "Codex is configured with a different binary path."
      if confirm "Update Codex to use $binary_path?"; then
        sed -i.bak "s|command = \".*\"|command = \"$binary_path\"|" "$codex_config" && rm -f "${codex_config}.bak"
        success "Codex path updated → $codex_config"
        configured=$((configured + 1))
      else
        info "Codex skipped — keeping existing configuration"
        configured=$((configured + 1))
      fi
    else
      info "Codex already configured — skipping"
      configured=$((configured + 1))
    fi
  fi

  if [ "$configured" -eq 0 ]; then
    printf "\n  No MCP clients auto-configured.\n"
    printf "  Add Sentinel manually to your MCP client config:\n"
    print_manual_client_examples "$binary_path" "$FILESYSTEM_ROOT"
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  printf "\n  ${BOLD}🛡️  MCP Sentinel — Installer${NC}\n\n"

  detect_platform

  if [ "$SKIP_BINARY" = "1" ]; then
    # ── Homebrew / existing install mode ──────────────────────────────────
    info "Skipping binary download (SKIP_BINARY=1)"
    if ! command -v "$BINARY" >/dev/null 2>&1; then
      fatal "$BINARY not found in PATH. Install it first (e.g. brew install mcp-sentinel)."
    fi
    info "Using binary : $(command -v $BINARY)"
    echo ""
  else
    # ── Full install ──────────────────────────────────────────────────────
    local version
    version=$(get_version)

    info "Version  : $version"
    info "Platform : $PLATFORM"
    info "Install  : $INSTALL_DIR/$BINARY"
    echo ""

    local base_url="https://github.com/$REPO/releases/download/$version"
    local binary_file="${BINARY}-${version}-${PLATFORM}"

    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    info "Downloading binary..."
    download "${base_url}/${binary_file}" "$tmp_dir/$BINARY"

    info "Downloading checksums..."
    download "${base_url}/checksums.txt" "$tmp_dir/checksums.txt"

    verify_checksum "$tmp_dir/$BINARY" "$tmp_dir/checksums.txt" "$binary_file"

    chmod +x "$tmp_dir/$BINARY"
    if [ -w "$INSTALL_DIR" ]; then
      mv "$tmp_dir/$BINARY" "$INSTALL_DIR/$BINARY"
    else
      info "Installing to $INSTALL_DIR (requires sudo)..."
      sudo mv "$tmp_dir/$BINARY" "$INSTALL_DIR/$BINARY"
    fi
    success "Binary installed → $INSTALL_DIR/$BINARY"
  fi

  # ── ~/.sentinel/ setup ────────────────────────────────────────────────────
  ensure_sentinel_defaults

  # ── Configure MCP clients ─────────────────────────────────────────────────
  configure_clients

  # ── Done ──────────────────────────────────────────────────────────────────
  printf "\n  ${GREEN}${BOLD}✅ MCP Sentinel ${version:-$(command -v $BINARY)} configured successfully!${NC}\n\n"
  printf "  Next steps:\n"
  printf "    1. Edit ${BOLD}~/.sentinel/config.json${NC} — set default_filesystem_root for server-filesystem\n"
  printf "    2. Edit ${BOLD}~/.sentinel/rules.json${NC}  — customize security rules\n"
  printf "    3. Open ${BOLD}http://127.0.0.1:7438${NC}   — Live Monitor (while Sentinel is running)\n\n"
  printf "  Documentation: https://github.com/$REPO\n\n"
}

main
