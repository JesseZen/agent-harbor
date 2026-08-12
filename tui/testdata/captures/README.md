# Terminal capture review set

These deterministic captures are generated through the public Admin protocol
against `internal/testcore`, a same-UID Unix-socket fake built only from the
checked-in OpenAPI client. No proprietary Core source is used.

Each required terminal size contains:

- `core-sessions.ansi`: the global-tab application on Sessions;
- `core-dialog.ansi`: the same Core-backed Home with Agent Deck's New Session
  dialog open;
- `agent-deck.ansi`: `Home` rendered from an archive of the exact unmodified
Agent Deck v1.9.73 commit `2eedbc1ff60bcc23dd3f97848517b571e5f74ab9`;
- `agent-deck-dialog.ansi`: the dialog rendered by that exact upstream source;
- `side-by-side.ansi`: Core-backed Sessions and the direct Agent Deck Home on
  the same rows.
- `dialog-side-by-side.ansi`: the Core-backed and direct Agent Deck New
  Session dialogs on the same rows.
- `side-by-side.png` and `dialog-side-by-side.png`: image renderings of those
  exact terminal frames.

The sizes are `160x45`, `120x30`, `90x30`, and `70x30`. At 70 columns the
unchanged Agent Deck responsive layout stacks Sessions and Preview; wider
captures retain the split tree/preview composition. All sizes retain the
header, group/session tree, contextual footer, and original dialog.

Regenerate and review intentionally with:

```sh
./scripts/generate-upstream-captures.sh
UPDATE_GOLDEN=1 go test ./internal/app \
  -run '^TestFakeCoreGoldenCapturesMatchAgentDeckAtRequiredSizes$' -count=1
./scripts/render-captures.sh
```
