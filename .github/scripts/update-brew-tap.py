#!/usr/bin/env python3
"""Publishes or updates the mcp-sentinel Homebrew formula via the GitHub API."""
import os, base64, json, urllib.request, urllib.error

token        = os.environ["TAP_TOKEN"]
version      = os.environ["VERSION"]          # e.g. v1.0.0
version_bare = version.lstrip("v")            # e.g. 1.0.0
base_url     = os.environ["BASE_URL"]

shas = {
    "darwin_amd64": os.environ["SHA_DARWIN_AMD64"],
    "darwin_arm64": os.environ["SHA_DARWIN_ARM64"],
    "linux_amd64":  os.environ["SHA_LINUX_AMD64"],
    "linux_arm64":  os.environ["SHA_LINUX_ARM64"],
}

print(f"version:      {version}")
print(f"version_bare: {version_bare}")
print(f"base_url:     {base_url}")
for k, v in shas.items():
    print(f"sha {k}: {v or '(empty!)'}")

formula = (
    "class McpSentinel < Formula\n"
    '  desc "Community Edition local MCP proxy with static regex blocking"\n'
    '  homepage "https://github.com/Aether-Labs-Studio/mcp-sentinel"\n'
    f'  version "{version_bare}"\n'
    '  license "MIT"\n'
    "\n"
    "  on_macos do\n"
    "    on_arm do\n"
    f'      url "{base_url}/mcp-sentinel-{version}-darwin-arm64"\n'
    f'      sha256 "{shas["darwin_arm64"]}"\n'
    "    end\n"
    "    on_intel do\n"
    f'      url "{base_url}/mcp-sentinel-{version}-darwin-amd64"\n'
    f'      sha256 "{shas["darwin_amd64"]}"\n'
    "    end\n"
    "  end\n"
    "\n"
    "  on_linux do\n"
    "    on_arm do\n"
    f'      url "{base_url}/mcp-sentinel-{version}-linux-arm64"\n'
    f'      sha256 "{shas["linux_arm64"]}"\n'
    "    end\n"
    "    on_intel do\n"
    f'      url "{base_url}/mcp-sentinel-{version}-linux-amd64"\n'
    f'      sha256 "{shas["linux_amd64"]}"\n'
    "    end\n"
    "  end\n"
    "\n"
    "  def install\n"
    '    bin.install Dir["mcp-sentinel*"].first => "mcp-sentinel"\n'
    "  end\n"
    "\n"
    "  def caveats\n"
    "    <<~EOS\n"
    "      To configure your MCP clients (Claude Code, Cursor, Gemini CLI, etc.), run:\n"
    "        curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | SKIP_BINARY=1 sh\n"
    "\n"
    "      This will auto-detect installed clients and register the Community Edition\n"
    "      mcp-sentinel binary in each one.\n"
    "      The binary path used will be: #{opt_bin}/mcp-sentinel\n"
    "    EOS\n"
    "  end\n"
    "\n"
    "  test do\n"
    '    output = shell_output("#{bin}/mcp-sentinel 2>&1", 1)\n'
    '    assert_match "usage", output\n'
    "  end\n"
    "end\n"
)

content = base64.b64encode(formula.encode()).decode()
api_url = "https://api.github.com/repos/Aether-Labs-Studio/homebrew-tap/contents/Formula/mcp-sentinel.rb"
headers = {
    "Authorization": f"token {token}",
    "Accept": "application/vnd.github.v3+json",
    "Content-Type": "application/json",
}

# Fetch existing file SHA (required for updates, omitted for creates)
req = urllib.request.Request(api_url, headers=headers)
try:
    with urllib.request.urlopen(req) as r:
        existing_sha = json.load(r)["sha"]
except urllib.error.HTTPError:
    existing_sha = None

payload = {"message": f"chore: update mcp-sentinel to {version}", "content": content}
if existing_sha:
    payload["sha"] = existing_sha

req = urllib.request.Request(api_url, data=json.dumps(payload).encode(), headers=headers, method="PUT")
try:
    with urllib.request.urlopen(req) as r:
        print(f"Formula published: HTTP {r.status}")
except urllib.error.HTTPError as e:
    print(f"API error: HTTP {e.code}")
    print(e.read().decode())
    raise
