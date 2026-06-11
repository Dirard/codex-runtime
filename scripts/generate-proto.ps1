$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
$module = "github.com/Dirard/codex-runtime"

$protoc = $env:PROTOC
if ([string]::IsNullOrWhiteSpace($protoc)) {
    $protoc = "protoc"
}

$goPath = (& go env GOPATH).Trim()
$goBin = Join-Path $goPath "bin"
$env:PATH = "$goBin;$env:PATH"

& $protoc `
    -I (Join-Path $repoRoot "proto") `
    --go_out=$repoRoot `
    --go_opt=module=$module `
    --go-grpc_out=$repoRoot `
    --go-grpc_opt=module=$module `
    (Join-Path $repoRoot "proto/codex_control/v1/codex_control.proto")
