class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.8"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.8/agent-harbor-v0.1.0-dev.8-darwin-arm64.tar.gz"
      sha256 "c4ab7b0a1ef758db80263b31b0a801c64f8cc6f17152193f2aa78e04905949f6"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.8/agent-harbor-v0.1.0-dev.8-darwin-amd64.tar.gz"
      sha256 "1f7dc429e8ea2848d30dac9ec37d05ca75860b39fd81bac4ffb28768fedbd44c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.8/agent-harbor-v0.1.0-dev.8-linux-arm64.tar.gz"
      sha256 "c462f5f5c1aa50f56c23768080d528642e69e81abfc9b7a93b58be1f67d89457"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.8/agent-harbor-v0.1.0-dev.8-linux-amd64.tar.gz"
      sha256 "7085b36ef28fcaf75cc6844e3f5590475fed1366be8c09a4843e6a9702704203"
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
