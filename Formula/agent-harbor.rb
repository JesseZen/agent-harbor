class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.10"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.10/agent-harbor-v0.1.0-dev.10-darwin-arm64.tar.gz"
      sha256 "22f55bd0b61019c74e835e4c620e42fc5a57597d0787a19ebd0b0131ec57834f"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.10/agent-harbor-v0.1.0-dev.10-darwin-amd64.tar.gz"
      sha256 "98e74e4386379dd77a49f59368c97fe6bd8780902b777c778e88f3cc66e802b3"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.10/agent-harbor-v0.1.0-dev.10-linux-arm64.tar.gz"
      sha256 "465ab4f9f719e87bf457a0565f18c616668be135ae50c7181730f8048462ce39"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.10/agent-harbor-v0.1.0-dev.10-linux-amd64.tar.gz"
      sha256 "90b94c580087765a1a167a1838384b6f7990b73b3141e50619b9d41a9c3944db"
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
