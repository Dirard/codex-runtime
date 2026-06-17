# Workflow Scaffold

Copies a checked-in starter workflow to an app-owned folder.

```powershell
go run .\examples\workflow-scaffold -source .\examples\workflows\writer-notes -target .\.local\workflows\writer-notes
```

Safety behavior:

- requires explicit source and target;
- creates target directories as needed;
- refuses to overwrite existing files unless `-overwrite` is passed;
- copies workflow files only;
- never reads, imports or mutates global `CODEX_HOME`;
- never copies token-source files or resolved secret values.
