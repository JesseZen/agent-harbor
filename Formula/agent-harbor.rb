class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.7"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.7/agent-harbor-v0.1.0-dev.7-darwin-arm64.tar.gz"
      sha256 "6b4ecc0916f7dcd7da3a26be0b0813a34021295534f9e5f810ed1fca7a9c94f9"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.7/agent-harbor-v0.1.0-dev.7-darwin-amd64.tar.gz"
      sha256 "83c942cccf6043eb19d58ff73a0af47918c415659f182c3eba50522e0da1ba03"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.7/agent-harbor-v0.1.0-dev.7-linux-arm64.tar.gz"
      sha256 "9025650c064c358d8424e47fbe3d1a1e780cc551fa104747e41bb11e2fbe6686"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.7/agent-harbor-v0.1.0-dev.7-linux-amd64.tar.gz"
      sha256 "01e02815f26529359959dbfc1f5c24d1166e4b203d131ede60318d824d819782"
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
