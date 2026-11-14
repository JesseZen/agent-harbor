# Agent Harbor

Agent Harbor manages AI CLI sessions in Herdr and connects them to either a client's native configuration or a selected Magpie model or routing group. Herdr owns terminal sessions and client discovery; Magpie owns provider configuration and routing.

## Development status

This repository contains an **experimental development candidate**, not a supported release. Exact conversation recovery relies on a local Herdr query patch which has not been submitted to, accepted by, or released by Herdr. See [Herdr integration](docs/herdr-integration.md). Native macOS arm64 and Linux amd64 candidate acceptance used isolated real clients, real Magpie and simulated providers. Other architectures have build/package evidence only. Future behavior and client compatibility may change.

## Code and testing

The active Go module is in `bridge/`. Go 1.26 is required. Run `GOWORK=off go test ./...` from that directory using isolated HOME, XDG, temporary directories and client homes. Product runtime and packaging require Herdr, Magpie and a supported native client. See the [development candidate guide](bridge/PACKAGE-README.md) and [acceptance guide](bridge/tests/README.md).

This snapshot replaces a previous product direction. The former Core, TUI and launcher are not part of this product.

## License

Agent Harbor is available under the [MIT License](LICENSE). Bundled dependencies and Herdr's separately licensed patch have their own notices in [bridge/licenses](bridge/licenses/).
