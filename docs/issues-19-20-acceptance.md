# Profiles and Codex accounts: acceptance record

Date: 2026-11-16. Integrated local main: `8fc230e43b027b017e1733643da589d9a2b99018`.
Issues: [#19](https://github.com/JesseZen/agent-harbor/issues/19),
[#20](https://github.com/JesseZen/agent-harbor/issues/20).

The maintainer started the integrated candidate through the supplied launch
script on macOS and reported that manual acceptance passed, with no major
functional problems. The maintainer requested closing the implementation issues.
This records human acceptance of the exercised candidate; it does not infer
which individual account, remote, recovery or failure scenarios were exercised.
No private account identifiers, credentials or Conversation content were collected.

Prior implementation evidence includes Linux amd64 tests with Codex 0.169.0,
Profile A/B requests and separate history/SQLite, concurrent real-client UI
sessions, exact A/B recovery after workspace/gateway restart, the full-state UI
regression gate, integrated Go and targeted race tests, and Linux/macOS arm64
package builds. The macOS package was cross-built before the maintainer's trial.

The original exhaustive real-account grant/refresh/request-identity matrix,
remote Profile/account journeys and native package/platform matrix have not
acquired separate automated acceptance evidence from this report. Keep those
validation requirements when expanding supported-platform or real-account
claims. Manual acceptance and issue closure do not release the product or
establish upstream acceptance of the Herdr/Magpie candidate patches.
