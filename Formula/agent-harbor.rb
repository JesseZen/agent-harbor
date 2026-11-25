class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "0.1.0-dev.15"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.15/agent-harbor-v0.1.0-dev.15-darwin-arm64.tar.gz"
      sha256 "7ea178f0e448f8faba7ba4bd62b415d4fbd21d6955bd64cf41bbb6f914d9a312"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.15/agent-harbor-v0.1.0-dev.15-darwin-amd64.tar.gz"
      sha256 "584095b942f5732f9975517f1a49d6f59c1bf35e2aa40a391f9ac3533b4f549c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.15/agent-harbor-v0.1.0-dev.15-linux-arm64.tar.gz"
      sha256 "48eaf4231e27e5174d7e7bf33d5a5ff6cdab7d2e34577b9ccd1a867420093a63"
    else
      url "https://github.com/JesseZen/agent-harbor/releases/download/v0.1.0-dev.15/agent-harbor-v0.1.0-dev.15-linux-amd64.tar.gz"
      sha256 "fe4c9a06067bea1ba6249256670152c86bc3ad4a531d733132bd94ef763ffc95"
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
