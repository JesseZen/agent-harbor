class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.11"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.11/agent-harbor-v0.1.0-dev.11-darwin-arm64.tar.gz"
      sha256 "166703aefd8b68149242c750a142800671c3ac66d397ef014603840d8d0e98e6"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.11/agent-harbor-v0.1.0-dev.11-darwin-amd64.tar.gz"
      sha256 "456437a98c05634b4e06ded66788ad3df985f9337673705653a6db453685fdbd"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.11/agent-harbor-v0.1.0-dev.11-linux-arm64.tar.gz"
      sha256 "8c993bcbb57d01168399051d97ddff248a1aef2bb2251563d9af5a9889903cdf"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.11/agent-harbor-v0.1.0-dev.11-linux-amd64.tar.gz"
      sha256 "25ad4d6c3ece79458c3fa014b035fe0167cb0bae16c39cdc7f140637b031314e"
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
