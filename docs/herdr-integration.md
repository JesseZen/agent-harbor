# Herdr integration and local patch

Agent Harbor uses Herdr for terminal sessions, client discovery and conversation identity. Its current experimental recovery flow additionally needs a read-only command that asks Herdr to produce its existing client-specific resume command from a structured session reference. Harbor performs its own identity and connection review before invoking recovery; the query itself is not a recovery gate.

The candidate patch is [herdr-resume-command.patch](herdr-resume-command.patch), based on Herdr commit `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`; the resulting candidate source is `60a5316204b0c538229defb2628e50660a469b85`. The patch is provided for review and possible upstream contribution. It has not been submitted, accepted or released by Herdr. Do not replace a supported installation with this development candidate.

Herdr is licensed under Apache-2.0; its license and bundled dependency notices are in `bridge/licenses/`. Magpie is distributed under its own license, also included in that directory.

## Remote workspace candidate

Apply [herdr-remote-executable.patch](herdr-remote-executable.patch) to the query
candidate `60a5316204b0c538229defb2628e50660a469b85` to build this branch's remote
dependency. It adds an optional `--remote-executable /absolute/posix/path` to
`herdr machine add`, persisted per machine. Setup, live client reconnects,
machine status and remote API discovery use that exact path. An explicit path
does not fall back to PATH discovery or an automatic installation. API metadata
cached for another path is discarded. Existing profiles without the field keep
their normal discovery behavior. This local patch is not submitted, accepted or
released upstream and adds no server wire-protocol changes.

Remote candidate assets retain the query candidate's `.source` value and must
also include `.remote-patch.sha256`, the SHA-256 of the exact additional patch.
These are local operator attestations, not upstream signatures. See
[remote workspaces](remote-workspaces.md) for setup and recovery boundaries.
