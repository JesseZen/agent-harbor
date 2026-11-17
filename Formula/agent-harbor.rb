class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.3"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.3/agent-harbor-v0.1.0-dev.3-darwin-arm64.tar.gz"
      sha256 "901fb7b6c67653a166b7d6f8db84c5e2ea2d812801f2306289ba91924fcf5ef0"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.3/agent-harbor-v0.1.0-dev.3-darwin-amd64.tar.gz"
      sha256 "dbd1feb1491896afcd3af472e7cf7151056ee2768acb1dde9458c19528d91a4d"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.3/agent-harbor-v0.1.0-dev.3-linux-arm64.tar.gz"
      sha256 "1a9e6c7ded48f88050dad9d33643fba36b321069e37de9140d56d86bc40887ad"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.3/agent-harbor-v0.1.0-dev.3-linux-amd64.tar.gz"
      sha256 "b0d8f27a126785e011330b2afc49de6defd848e48501274f2e27ea556d7bfb43"
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
