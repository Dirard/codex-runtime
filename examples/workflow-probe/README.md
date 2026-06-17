# Workflow Probe

Small readiness probe for a private Codex runtime gateway. It reads the gateway
credential from a token-source file and does not print the token.

```powershell
go run .\examples\workflow-probe -gateway "127.0.0.1:5575" -token-source ".\.local\gateway.token" -session-group "workflow-smoke-session" -workspace "workflow-smoke-workspace"
```

Use this before `workflow-smoke` when checking whether the gateway is reachable
from the backend environment.
