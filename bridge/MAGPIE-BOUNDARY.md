# Magpie connection boundary

ADR 0006 accepts Magpie auxiliary routing; ADR 0007 defines verified instances and Harbor-owned lifecycle. No upstream authorization patch is a product prerequisite.

Harbor records the selected model or group and the instance configuration location and address. It checks identity and catalog availability before launch and recovery. A stable identifier does not prove unchanged provider credentials or group membership. Review the intended destination in Magpie before resuming.

Magpie may send auxiliary requests containing parts of the conversation to other models or providers. Harbor explains this before first use and retains a reminder. It does not enforce per-session destination authorization inside Magpie. Details distinguish the selected connection from observed main and auxiliary routes, where the pinned Magpie API provides them.

Managed Magpie runs with its own home, Codex home and portable configuration, independently of Herdr and management UI. Stop it explicitly with `agent-harbor magpie stop`. External instances remain user-managed; Harbor never stops them. Management opens only against verified instance evidence and a serving gateway. Closing an original external TUI that hosts a gateway can stop that gateway.

Managed Magpie's management UI uses the same isolated client homes even when an older gateway process inherited client-specific paths. External management follows the external instance's environment and warns that Magpie's agent controls may change those clients' settings. Existing native configuration is not rewritten automatically.

For Codex, the current candidate copies the selected instance's own Codex catalog into a private temporary file for the run and uses a separate Codex profile per Harbor session. It reviews the profile's saved model and reasoning effort during recovery; it does not infer a selection from Magpie's downstream routing. Codex's `s` picker choice does not change the profile default, so the candidate's profile-based recovery can miss it. Codex 0.167.0 can recover an `s` choice for the same conversation when no recovery-time model override is supplied; the Harbor integration remains to be validated. Catalog and profile behavior is version-specific and must be rechecked when upgrading Codex or Magpie.

Managed Magpie Library's global agent targets use the managed home. Its project targets can place skills and MCP configuration into a project selected by the user; this changes project files and does not install Codex plugins. Harbor does not migrate skills or plugins automatically.

See [first-use guide](PACKAGE-README.md) for operation and recovery. Earlier proposed gateway patches are historical research, not bundled dependencies or guarantees.
