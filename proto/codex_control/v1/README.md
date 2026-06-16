# codex_control.v1

This directory contains the public Stage 2 protobuf contract for the
codex-runtime gateway. Regenerate Go bindings from the module root with:

```powershell
.\scripts\generate-proto.ps1
```

The script uses the Docker compiler image `dion_chats/proto/compile:1.7`.
If the image is missing locally, the script builds it from
`scripts/proto-compiler.Dockerfile`.

Set `CODEX_RUNTIME_PROTOC_IMAGE` only when intentionally testing another
compiler image.
