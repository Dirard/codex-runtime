$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
$module = "github.com/Dirard/codex-runtime"
$image = $env:CODEX_RUNTIME_PROTOC_IMAGE
if ([string]::IsNullOrWhiteSpace($image)) {
    $image = "dion_chats/proto/compile:1.7"
}

function Invoke-Docker {
    & docker @args
    if ($LASTEXITCODE -ne 0) {
        throw "docker command failed: docker $($args -join ' ')"
    }
}

Invoke-Docker version --format "{{.Server.Version}}" | Out-Null

$hasImage = (& docker image ls --filter "reference=$image" -q).Trim()
if ([string]::IsNullOrWhiteSpace($hasImage)) {
    $dockerfile = Join-Path $scriptDir "proto-compiler.Dockerfile"
    if (-not (Test-Path -LiteralPath $dockerfile)) {
        throw "missing proto compiler Dockerfile: $dockerfile"
    }
    Invoke-Docker build -t $image -f $dockerfile $scriptDir
}

$mountRoot = $repoRoot -replace "\\", "/"
Invoke-Docker run --rm `
    -v "${mountRoot}:/app" `
    -w /app `
    $image `
    protoc `
    -I /app/proto `
    --go_out=/app `
    --go_opt=module=$module `
    --go-grpc_out=/app `
    --go-grpc_opt=module=$module `
    /app/proto/codex_control/v1/codex_control.proto
