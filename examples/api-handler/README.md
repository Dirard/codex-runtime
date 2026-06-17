# Workflow API Handler

Minimal backend-only web integration example:

`browser -> app backend /workflow -> Go SDK -> private gateway -> Codex workflow`

The browser never receives gateway credentials and never connects to the gateway.

## Environment
```powershell
$env:CODEX_RUNTIME_GATEWAY_ADDR = "127.0.0.1:5575"
$env:CODEX_RUNTIME_TOKEN_SOURCE = ".\.local\gateway.token"
$env:CODEX_RUNTIME_SESSION_GROUP = "workflow-smoke-session"
$env:CODEX_RUNTIME_WORKSPACE = "workflow-smoke-workspace"
$env:CODEX_WORKFLOW_NAMESPACE = "examples"
$env:CODEX_WORKFLOW_ID = "writer-notes"
$env:CODEX_WORKFLOW_DIR = ".\.local\workflows\writer-notes"
```

## Run
```powershell
go run .\examples\api-handler
```

First turn:

```powershell
Invoke-WebRequest "http://127.0.0.1:8080/workflow?prompt=Summarize%20the%20harbor%20note"
```

Continuation:

```powershell
Invoke-WebRequest "http://127.0.0.1:8080/workflow?chat_id=<stored-chat-id>&prompt=Continue%20for%20a%20newsletter"
```

The response starts with `chat_id=...` and streams assistant text. The example
maps typed SDK errors to safe HTTP responses with a stable code, workflow code
and next action when the gateway provides one.
