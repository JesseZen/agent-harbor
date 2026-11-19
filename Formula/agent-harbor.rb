class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.9"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.9/agent-harbor-v0.1.0-dev.9-darwin-arm64.tar.gz"
      sha256 "3b2090302b900bb431b5f10bb403baab3425233f708ee4219be6edd9874fde6b"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.9/agent-harbor-v0.1.0-dev.9-darwin-amd64.tar.gz"
      sha256 "af1685d38989ea7ad8f0ed25a2751fa35d247a196959c22e72128909eb9b2b0b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.9/agent-harbor-v0.1.0-dev.9-linux-arm64.tar.gz"
      sha256 "80763b0e5acb9f001a362219a904e74a07df49fab21e9df403b29a982187ca3b"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.9/agent-harbor-v0.1.0-dev.9-linux-amd64.tar.gz"
      sha256 "bc886b551756a3e2bbc8f65b07f7a0f0dff85dfbd55c0b033d4ccb4a7c2f350d"
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
