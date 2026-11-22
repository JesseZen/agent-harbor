class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.14"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.14/agent-harbor-v0.1.0-dev.14-darwin-arm64.tar.gz"
      sha256 "138edeb10ffe41ac9c0246f844af77d82c14c577fd50fd8f79aa207a0ce8e2d4"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.14/agent-harbor-v0.1.0-dev.14-darwin-amd64.tar.gz"
      sha256 "bd973596305e22361566010224df721d201265c890f726b292f6bfa758701f32"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.14/agent-harbor-v0.1.0-dev.14-linux-arm64.tar.gz"
      sha256 "be2fb1100dd99bd2cd314b9bd16c3b2877ad7a9df278a899464c1249ac6e864a"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.14/agent-harbor-v0.1.0-dev.14-linux-amd64.tar.gz"
      sha256 "a520a452e2063731edd91ee31eade0d6e9378e5371bf500b3bf1692418aa4dc9"
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
