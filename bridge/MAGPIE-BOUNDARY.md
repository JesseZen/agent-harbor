# Magpie connection boundary

ADR 0006 accepts Magpie auxiliary routing; ADR 0007 defines verified instances and Harbor-owned lifecycle. No upstream authorization patch is a product prerequisite.

Harbor records the selected model or group and the instance configuration location and address. It checks identity and catalog availability before launch and recovery. A stable identifier does not prove unchanged provider credentials or group membership. Review the intended destination in Magpie before resuming.

Magpie may send auxiliary requests containing parts of the conversation to other models or providers. Harbor explains this before first use and retains a reminder. It does not enforce per-session destination authorization inside Magpie. Details distinguish the selected connection from observed main and auxiliary routes, where the pinned Magpie API provides them.

Managed Magpie runs with its own home, Codex home and portable configuration, independently of Herdr and management UI. Stop it explicitly with `agent-harbor magpie stop`. External instances remain user-managed; Harbor never stops them. Management opens only against verified instance evidence and a serving gateway. Closing an original external TUI that hosts a gateway can stop that gateway.

See [first-use guide](PACKAGE-README.md) for operation and recovery. Earlier proposed gateway patches are historical research, not bundled dependencies or guarantees.
