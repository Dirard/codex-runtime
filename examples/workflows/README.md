# Workflow Starters

These folders are app-owned Codex workflow packages. Copy one to your app and
initialize it through `client.InitWorkflow(ctx, codex.WorkflowDir{...})`.

## Starters
- `plain-chat`: smallest assistant workflow with one agent.
- `writer-notes`: deterministic notes workflow with a local stdio MCP fixture,
  a `writer-notes` skill and shipped non-secret references.
- `visibility-alpha` and `visibility-beta`: paired full-e2e fixtures used to
  prove that workflow-scoped agents and skills do not bleed across workflows.

## Folder Contract
- `config.toml` is the workflow root config.
- `AGENTS.md` contains workflow-scoped Codex project instructions.
- `agents/` contains agent profiles.
- `skills/` contains optional skill folders with `SKILL.md`.
- `references/` may contain small shipped non-secret reference files.
- `tools/` may contain materialized workflow helpers allowed by gateway policy.

The gateway revalidates every package, rejects unsafe paths and raw secrets, and
derives its own storage key from namespace + workflow ID.

## Quick Copy
```powershell
go run .\examples\workflow-scaffold -source .\examples\workflows\writer-notes -target .\.local\workflows\writer-notes
```

The scaffold does not read or mutate global `CODEX_HOME` and will not overwrite
target files unless `-overwrite` is passed.
