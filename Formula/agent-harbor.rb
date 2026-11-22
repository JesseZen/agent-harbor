class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.12"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.12/agent-harbor-v0.1.0-dev.12-darwin-arm64.tar.gz"
      sha256 "fc8791ec1c88373f28c97572ef68d1233bae82cf95ad9f1470b7010ef0fe3e58"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.12/agent-harbor-v0.1.0-dev.12-darwin-amd64.tar.gz"
      sha256 "db74d6d4a0dfb3bda76cfc43582a33db4f904a8f091e69bd6bb245cfc44469ee"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.12/agent-harbor-v0.1.0-dev.12-linux-arm64.tar.gz"
      sha256 "f557b263c61500de6ec4c19f2666cf0d877ce7c1bd2dad5c3a4cdf04536c3eb4"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.12/agent-harbor-v0.1.0-dev.12-linux-amd64.tar.gz"
      sha256 "f5b5bd7a16e3a6d0c81b843f71f28854362875cc50fb419607d2d9ef196211ab"
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
