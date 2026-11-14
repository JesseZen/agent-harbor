# Product outline

Agent Harbor helps a user create, inspect, resume and manage AI CLI sessions inside an owned Herdr workspace. Herdr manages terminal and client capabilities; Magpie provides optional provider and routing configuration.

A session records a project, client, conversation reference and connection intent. A native connection uses the client's own settings and login. A Magpie connection names a verified instance and a model or routing group. Different sessions can use different connections. Auxiliary Magpie requests may route portions of a conversation to other providers and must be explained to users.

On restart or recovery Harbor must preserve the intended conversation and connection. If either cannot be established, show the uncertainty and require explicit review; never silently substitute a client, account, route or conversation. External Herdr agents may be listed and focused without claiming ownership or inferring their connection.

Use documented upstream integration interfaces. Unsupported operations must be presented clearly. Keep user credentials and conversations out of logs and test fixtures.
