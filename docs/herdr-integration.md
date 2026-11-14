# Herdr integration and local patch

Agent Harbor uses Herdr for terminal sessions, client discovery and conversation identity. Its current experimental recovery flow additionally needs a read-only command that asks Herdr to produce its existing client-specific resume command from a structured session reference. Harbor performs its own identity and connection review before invoking recovery; the query itself is not a recovery gate.

The candidate patch is [herdr-resume-command.patch](herdr-resume-command.patch), based on Herdr commit `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`; the resulting candidate source is `60a5316204b0c538229defb2628e50660a469b85`. The patch is provided for review and possible upstream contribution. It has not been submitted, accepted or released by Herdr. Do not replace a supported installation with this development candidate.

Herdr is licensed under Apache-2.0; its license and bundled dependency notices are in `bridge/licenses/`. Magpie is distributed under its own license, also included in that directory.
