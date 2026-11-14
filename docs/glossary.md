# Agent Harbor

Product language for Harbor's managed workspace and its connections to Herdr and Magpie.

## Language

**Agent Client（客户端）**:
An interactive AI coding tool used in a workspace, such as Codex, Claude Code or Pi. A client is distinct from the model or provider it connects to.
_Avoid_: Model, provider, agent when the intended meaning is the client application.

**Harbor Session（Harbor 会话）**:
A named workspace record connecting a project, an agent client, a conversation and the user's connection intent. Reopening a session preserves those choices unless the user explicitly changes them.

**Conversation（对话）**:
The ongoing exchange and history belonging to an agent client. A terminal pane is a place to interact with a conversation, not the conversation itself.

**Native Connection（原生连接）**:
Use of an agent client's own configuration and login. This does not imply a local model.
_Avoid_: Local model, offline mode.

**Magpie Connection（Magpie 连接）**:
Use of a selected Magpie instance and its model or routing group. A client's native connection support does not establish its Magpie connection support.

**Connection Intent（连接意图）**:
The connection the user chose for a session. It is distinct from the provider or model Magpie actually selected for a particular request.

**Bundled Magpie（随包附带的 Magpie）**:
Magpie supplied with the Harbor installation as a separate upstream program. Bundling alone does not determine who manages a running instance.
_Avoid_: Embedded Magpie, 内置 Magpie without specifying whether it means bundling or source embedding.

**Harbor-managed Magpie Instance（Harbor 托管的 Magpie 实例）**:
A Magpie instance whose configuration and lifecycle belong to Harbor. An external Magpie instance remains under the user's management.

**Client Capability（客户端能力）**:
A supported operation for a particular client and connection, such as starting a conversation, identifying it or resuming it. Support for one operation does not imply support for the others.
