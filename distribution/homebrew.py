#!/usr/bin/env python3
"""Generate the version-pinned Homebrew formula from the release checksum file."""
import pathlib,re,sys
version=sys.argv[1]
assert re.fullmatch(r'[a-zA-Z0-9._-]+',version)
sums=dict(line.split()[::-1] for line in pathlib.Path(sys.argv[2]).read_text().splitlines())
print('''class AgentHarbor < Formula
  desc "AI CLI sessions with Profiles, conversation recovery and optional routing"
  homepage "https://github.com/JesseZen/agent-harbor"
  version "%s"
  license "MIT"
''' % version.removeprefix('v'))
for platform,condition in [('darwin','on_macos'),('linux','on_linux')]:
    print(f'  {condition} do')
    for arch,branch in [('arm64','if Hardware::CPU.arm?'),('amd64','else')]:
        asset=f'agent-harbor-{version}-{platform}-{arch}.tar.gz'
        print(f'    {branch}\n      url "https://github.com/JesseZen/agent-harbor/releases/download/{version}/{asset}"\n      sha256 "{sums[asset]}"')
    print('    end\n  end\n')
print('''  def install
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
end''')
