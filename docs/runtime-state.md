# Runtime state contract

Harbor reads the supported Herdr public `agent.list` response once per list or
show query. The supported source baseline is `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`.
The public `AgentInfo` contract supplies `agent_status`, workspace, terminal,
tab and pane IDs, optional `agent_session`, `launch_pending` and
`interactive_ready`. No client files or process scanning supply activity.

`runtime.activity` preserves `idle`, `working`, `blocked`, `done` and `unknown`.
Missing or future values become `unknown`. `done` describes completed work in a
live agent, not process termination. Launch pending is shown as Starting;
interactive readiness is independent of activity.

`runtime.presence` is `observed`, `not_observed`, `not_started` or
`historical_exit` or explicitly reviewed `confirmed_exit`. A runner's saved exit has source `harbor.runner.history` and
its recorded time; it is not a current Herdr observation. Absence from a successful
list does not prove exit. Discovery errors preserve recorded sessions, mark
activity unknown, and expose `discovery_warning`; an empty list reports the
error directly. Observation timestamps apply to the response, not client work.

A conversation associates with a record only when workspace socket instance,
pane, tab, client and the official conversation reference agree with the
current run's stored, observed evidence. Initial unverified evidence cannot
associate a conversation. Terminal/workspace IDs must be available.
Before a conversation reference is available, a live run may instead attach as
`runtime.run_verified` when Herdr reports its process group in the foreground
and the process's start token matches the recorded run. This verifies the
terminal target, not the conversation: `runtime.associated` remains false and
recovery still requires separate review. An unreadable process, changed
foreground, ambiguous record or conflicting reference leaves it unassociated.
Process evidence is never an activity source.
External agents never acquire the record's connection or conversation intent.
Their IDs include instance, terminal and conversation identity, so replacement
invalidates an old target.

JSON remains additive: records retain existing fields and add `runtime` and
`health`. `status` now labels authoritative activity instead of replacing it
with a connection warning. `running` means a current associated or run-verified
Herdr observation (or an observed external agent), including done; false includes unknown and is
not permission to recover. Callers must migrate decisions to `runtime.presence`
and `runtime.activity` and retain explicit recovery checks. `reason`, `health`,
`recovery`, `external` and `runtime.associated` describe separate dimensions.

The recovery-command query bundled with the development candidate is a local
patch. It has not been accepted or released upstream.

The popup refreshes every two seconds through the same observation contract.
Only one refresh runs at once. Socket requests have a two-second deadline, a
64 KiB response bound and cancellation on popup exit. Errors invalidate cached
activity immediately; a recent observation may remain visible only as stale.
Reconnect retries automatically. Health checks run in one bounded background
worker with a ten-second cache so they cannot delay activity or keyboard input.
Search, forms and review intent survive refreshes. A vanished selection requires
an explicit new selection before any action.

Before entering recovery review, Harbor re-reads the selected record and runtime.
Confirmations retain their observed instance, workspace, terminal and reference;
changes invalidate approval. Focus rechecks the same target, including done.
An observed agent whose activity is unknown may still be focused after its target
identity is established. Unknown activity never authorizes launch or recovery.
Launch requires a never-started record or a recorded exit and a reachable workspace,
then repeats existing directory, connection, conversation, ownership and duplicate
checks at execution. A matching conversation observed in an unmanaged terminal
also prevents duplicate recovery. External agents remain read-only.

The supported Herdr agent action target is a pane ID or assigned agent name,
not its returned terminal ID. Harbor compares terminal identity before sending
the supported target. Herdr does not offer an identity-conditioned action
transaction; the observation and action are separate public requests.

After an abrupt workspace shutdown, absence alone can leave the previous run
unconfirmed. Inspect its restored idle shell with `c`; confirming the existing
close review records `confirmed_exit` evidence with source
`herdr.reviewed_idle_shell`. That review rechecks socket, terminal, tab, label,
shell process and approved record before closing. The evidence belongs to the
reviewed run and reference; a later start invalidates it. It is a historical,
explicit review decision, not a live activity observation. Normal runner exit
bookkeeping remains `historical_exit`. The reviewed exit is saved before the
close request, because closing the active pane can terminate the reviewing
popup. If the close fails, the exit review remains recorded and the failure is
reported separately. Both exit forms still require fresh workspace and
full recovery preflight before execution.

For isolated real runtime acceptance, run `tests/app_acceptance.py` with
`--full-state --replacement --renamed-codex`, the candidate executables, a native
Codex binary, and the built Herdr MCP entry point. It exercises a manually
started external client alongside the three owned connections, live refresh,
owned-socket disconnect/reconnect and explicit restored-shell end review.

A temporary missing socket invalidates identity observation. If the same socket
instance returns, the same run must report its original reference afresh before
association is restored. A final empty report cannot reuse pre-outage evidence.
A different socket instance, conflicting reference or changed pane association
continues to require explicit review.

When a saved reference is complete and the recorded evidence does not conflict,
the popup's `v` action offers an explicit review of that reference and its saved
connection. Accepting the review records a compatibility decision; it does not
claim a fresh Herdr observation or launch a client. Recovery still requires a
separate review and its full preflight. Missing references, conflicting evidence,
live targets and launches in progress cannot use this saved-reference review.
