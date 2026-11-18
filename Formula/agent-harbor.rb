class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.5"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.5/agent-harbor-v0.1.0-dev.5-darwin-arm64.tar.gz"
      sha256 "2420707160395702f2d7b8e3816f73591f9c917e40e78ae3dc5e9964fead2aa2"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.5/agent-harbor-v0.1.0-dev.5-darwin-amd64.tar.gz"
      sha256 "39cd4544ea1e96f64d439bd01ad44d86a1e84c05412fde2cc58bf4a1ba7a79f2"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.5/agent-harbor-v0.1.0-dev.5-linux-arm64.tar.gz"
      sha256 "8f08eb54e64b60696419b064cc51c6cc51af2d3607484798377cf4fadf9bc1af"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.5/agent-harbor-v0.1.0-dev.5-linux-amd64.tar.gz"
      sha256 "39a4a33ea36bdaf4061dee5b0302bf02f7ef114e9a58dbcb3bf6922844374b66"
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
