# This file is a reference template.
# The canonical formula lives at: https://github.com/Aether-Labs-Studio/homebrew-tap
# It is updated automatically by the release workflow on every new tag.
class McpSentinel < Formula
  desc "Community Edition local MCP proxy with static regex blocking"
  homepage "https://github.com/Aether-Labs-Studio/mcp-sentinel"
  version "1.0.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/Aether-Labs-Studio/mcp-sentinel/releases/download/v1.0.0/mcp-sentinel-v1.0.0-darwin-arm64"
      sha256 "PLACEHOLDER"
    end
    on_intel do
      url "https://github.com/Aether-Labs-Studio/mcp-sentinel/releases/download/v1.0.0/mcp-sentinel-v1.0.0-darwin-amd64"
      sha256 "PLACEHOLDER"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Aether-Labs-Studio/mcp-sentinel/releases/download/v1.0.0/mcp-sentinel-v1.0.0-linux-arm64"
      sha256 "PLACEHOLDER"
    end
    on_intel do
      url "https://github.com/Aether-Labs-Studio/mcp-sentinel/releases/download/v1.0.0/mcp-sentinel-v1.0.0-linux-amd64"
      sha256 "PLACEHOLDER"
    end
  end

  def install
    bin.install Dir["mcp-sentinel*"].first => "mcp-sentinel"
  end

  def caveats
    <<~EOS
      To configure your MCP clients (Claude Code, Cursor, Gemini CLI, etc.), run:
        curl -fsSL https://raw.githubusercontent.com/Aether-Labs-Studio/mcp-sentinel/main/install.sh | SKIP_BINARY=1 sh

      This will auto-detect installed clients and register the Community Edition
      mcp-sentinel binary in each one.
      The binary path used will be: #{opt_bin}/mcp-sentinel
    EOS
  end

  test do
    output = shell_output("#{bin}/mcp-sentinel 2>&1", 1)
    assert_match "usage", output
  end
end
