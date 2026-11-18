class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.6"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.6/agent-harbor-v0.1.0-dev.6-darwin-arm64.tar.gz"
      sha256 "9e3716421b78aba67e2ff6c995c5b13bf63167542857142acbea84039874162d"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.6/agent-harbor-v0.1.0-dev.6-darwin-amd64.tar.gz"
      sha256 "c298b1fe1ab016bd6220eeaa8750505d8c3bd2aabd78ae2e0b2434cac763a392"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.6/agent-harbor-v0.1.0-dev.6-linux-arm64.tar.gz"
      sha256 "eaf3b49fe2ec3116013cbbdff70930c7d2ad7e6d1b109a8829e04c9793e49dec"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.6/agent-harbor-v0.1.0-dev.6-linux-amd64.tar.gz"
      sha256 "12d447bb44a34dc627d901723aa54619da2cb413a9b080a1367fc6738553d131"
    end
  end

  def install
    libexec.install "agent-harbor", "herdr", "magpie", "codex-auth"
    bin.install_symlink libexec/"agent-harbor"
    bin.install_symlink libexec/"agent-harbor" => "ah"
    (pkgshare/"licenses").install Dir["licenses/*"]
  end

  def caveats
    <<~EOS
      Run ah or agent-harbor. Install the native Codex CLI separately.
      Update: brew upgrade agent-harbor. Uninstall: brew uninstall agent-harbor.
      Accounts, configuration and conversations are retained on uninstall.
      This is a development candidate with local Herdr and Magpie patches.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/ah version")
  end
end
