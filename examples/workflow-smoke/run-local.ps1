[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$Gateway = "127.0.0.1:5575",
    [string]$TokenSource = ".\.local\gateway.token",
    [string]$WorkflowDir = ".\examples\workflows\writer-notes",
    [string]$Namespace = "examples",
    [string]$WorkflowId = "writer-notes",
    [string]$SessionGroup = "workflow-smoke-session",
    [string]$Workspace = "workflow-smoke-workspace",
    [string]$TranscriptPath = ".\.local\workflow-smoke-transcript.txt",
    [switch]$StartGateway
)

$ErrorActionPreference = "Stop"
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

function RedactedCommand([string]$Command) {
    return $Command -replace [regex]::Escape($TokenSource), "<token-source>"
}

$probeArgs = @("run", ".\examples\workflow-probe", "-gateway", $Gateway, "-token-source", $TokenSource, "-session-group", $SessionGroup, "-workspace", $Workspace)
$smokeArgs = @("run", ".\examples\workflow-smoke", "-gateway", $Gateway, "-token-source", $TokenSource, "-session-group", $SessionGroup, "-workspace", $Workspace, "-namespace", $Namespace, "-workflow-id", $WorkflowId, "-workflow-dir", $WorkflowDir)
$probe = "go " + (($probeArgs | ForEach-Object { if ($_ -match "\s") { "`"$_`"" } else { $_ } }) -join " ")
$smoke = "go " + (($smokeArgs | ForEach-Object { if ($_ -match "\s") { "`"$_`"" } else { $_ } }) -join " ")

Write-Host "workflow-smoke local runner"
Write-Host "Prerequisites:"
Write-Host "- Codex runtime gateway listening on loopback: $Gateway"
Write-Host "- Token file exists at token-source path; token contents are never printed"
Write-Host "- Local Codex/app-server prerequisites are configured for the gateway"
Write-Host "- Writer-notes MCP helper is approved from materialized workflow tools/ path"
Write-Host "Redaction policy: bearer tokens, auth headers and token file contents are not written to output or transcript"
Write-Host "Transcript path: $TranscriptPath"
Write-Host "Commands:"
Write-Host ("- " + (RedactedCommand $probe))
Write-Host ("- " + (RedactedCommand $smoke))

if ($WhatIfPreference) {
    Write-Host "WhatIf: no gateway process or smoke command was started"
    return
}

if (-not (Test-Path -LiteralPath $TokenSource)) {
    throw "TokenSource file does not exist: $TokenSource"
}

$transcriptDir = Split-Path -Parent $TranscriptPath
if ($transcriptDir -and -not (Test-Path -LiteralPath $transcriptDir)) {
    New-Item -ItemType Directory -Path $transcriptDir | Out-Null
}

"workflow-smoke transcript (redacted)" | Set-Content -Encoding UTF8 -Path $TranscriptPath
"gateway=$Gateway" | Add-Content -Encoding UTF8 -Path $TranscriptPath
"token_source=<redacted>" | Add-Content -Encoding UTF8 -Path $TranscriptPath

if ($StartGateway) {
    Write-Host "StartGateway requested: start the gateway in a separate terminal or service manager for this repo's environment."
}

if ($PSCmdlet.ShouldProcess("workflow probe", "run token-safe readiness probe")) {
    & go @probeArgs | Tee-Object -Append -FilePath $TranscriptPath
}

if ($PSCmdlet.ShouldProcess("workflow smoke", "run token-safe workflow lifecycle smoke")) {
    & go @smokeArgs | Tee-Object -Append -FilePath $TranscriptPath
}
