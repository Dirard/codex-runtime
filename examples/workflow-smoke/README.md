# Workflow Smoke

Token-safe lifecycle smoke for the workflow SDK path:

`gateway ready -> InitWorkflow -> first streamed response -> history -> same-chat continuation`

Canonical local values:

- gateway: `127.0.0.1:5575`
- token source: `.local\gateway.token`
- session group: `workflow-smoke-session`
- workspace: `workflow-smoke-workspace`
- namespace: `examples`
- workflow ID: `writer-notes`
- workflow dir: `.local\workflows\writer-notes` after scaffold, or
  `examples\workflows\writer-notes` in-place

Dry-run the exact commands and redaction policy:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\workflow-smoke\run-local.ps1 -WhatIf
```

Run against a live local gateway:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\workflow-smoke\run-local.ps1 -TokenSource .\.local\gateway.token -WorkflowDir .\.local\workflows\writer-notes
```

The runner prints command shape with `<token-source>` and writes a redacted
transcript. It never writes token file contents or gateway auth headers.
