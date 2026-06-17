[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$Gateway = "127.0.0.1:5575",
    [string]$CodexBinary = "",
    [string]$CodexHome = "",
    [string]$CodexModel = "gpt-5.5",
    [string]$CodexModelProvider = "openai",
    [string]$WorkRoot = ".\.local\full-e2e",
    [string]$SessionGroup = "full-e2e-session",
    [string]$Workspace = "full-e2e-workspace",
    [string]$Namespace = "examples-full-e2e",
    [string]$WorkflowId = "plain-chat",
    [switch]$SkipLegacyTasks,
    [switch]$SkipMCPNegative,
    [switch]$SkipInterrupt,
    [switch]$SkipWorkflowVisibility,
    [switch]$SkipDelete,
    [switch]$UseSourceCodexHome,
    [switch]$KeepGateway
)

$ErrorActionPreference = "Stop"
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

function TomlString([string]$Value) {
    $escaped = $Value.Replace("\", "\\").Replace('"', '\"').Replace("`r", "\r").Replace("`n", "\n").Replace("`t", "\t")
    return '"' + $escaped + '"'
}

function New-SecretToken {
    $bytes = [byte[]]::new(32)
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    return ([System.BitConverter]::ToString($bytes) -replace "-", "").ToLowerInvariant()
}

function Resolve-CodexHome([string]$Requested) {
    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        return (Resolve-Path -LiteralPath $Requested).Path
    }
    if (-not [string]::IsNullOrWhiteSpace($env:CODEX_HOME)) {
        return (Resolve-Path -LiteralPath $env:CODEX_HOME).Path
    }
    $defaultHome = Join-Path $HOME ".codex"
    return (Resolve-Path -LiteralPath $defaultHome).Path
}

function Resolve-CodexBinary([string]$Requested) {
    $candidates = New-Object System.Collections.Generic.List[string]
    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        $candidates.Add($Requested)
    }
    if (-not [string]::IsNullOrWhiteSpace($env:CODEX_BINARY)) {
        $candidates.Add($env:CODEX_BINARY)
    }
    $commandExe = Get-Command codex.exe -ErrorAction SilentlyContinue
    if ($commandExe) {
        $candidates.Add($commandExe.Source)
    }
    $command = Get-Command codex -ErrorAction SilentlyContinue
    if ($command -and [System.IO.Path]::GetExtension($command.Source) -ieq ".exe") {
        $candidates.Add($command.Source)
    }
    foreach ($path in @(
        "D:\ai-apps\codex\codex-rs\target\debug\codex.exe",
        "D:\ai-apps\codex\codex-rs\target\release\codex.exe",
        "D:\ai-apps\codex\target\debug\codex.exe",
        "D:\ai-apps\codex\target\release\codex.exe"
    )) {
        $candidates.Add($path)
    }
    foreach ($candidate in $candidates) {
        if ([string]::IsNullOrWhiteSpace($candidate)) {
            continue
        }
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            $resolved = (Resolve-Path -LiteralPath $candidate).Path
            if ([System.IO.Path]::GetExtension($resolved) -ieq ".exe") {
                return $resolved
            }
        }
    }
    throw "Codex binary was not found. Pass -CodexBinary or set CODEX_BINARY."
}

function Test-TcpReady([string]$Address) {
    $endpoint = Get-GatewayEndpoint $Address
    $hostName = $endpoint.HostName
    $port = $endpoint.Port
    $client = [System.Net.Sockets.TcpClient]::new()
    try {
        $async = $client.BeginConnect($hostName, $port, $null, $null)
        if (-not $async.AsyncWaitHandle.WaitOne(500)) {
            return $false
        }
        $client.EndConnect($async)
        return $true
    } catch {
        return $false
    } finally {
        $client.Close()
    }
}

function Get-GatewayEndpoint([string]$Address) {
    $hostName = $Address
    $port = 0
    if ($Address.StartsWith("[")) {
        $closing = $Address.IndexOf("]")
        $hostName = $Address.Substring(1, $closing - 1)
        $port = [int]$Address.Substring($closing + 2)
    } else {
        $parts = $Address.Split(":")
        $hostName = $parts[0]
        $port = [int]$parts[$parts.Length - 1]
    }
    return [pscustomobject]@{ HostName = $hostName; Port = $port }
}

function Get-GatewayListenerPids([string]$Address) {
    $endpoint = Get-GatewayEndpoint $Address
    $connections = Get-NetTCPConnection -LocalPort $endpoint.Port -State Listen -ErrorAction SilentlyContinue
    $connections | Select-Object -ExpandProperty OwningProcess -Unique
}

function Stop-GatewayProcesses([string]$Address, $GoRunProcess, [bool]$KeepGateway) {
    if ($KeepGateway) {
        if ($GoRunProcess -and -not $GoRunProcess.HasExited) {
            Write-Host "gateway kept running: pid=$($GoRunProcess.Id)"
        }
        return
    }
    if ($GoRunProcess -and -not $GoRunProcess.HasExited) {
        Write-Host "stopping go run process pid=$($GoRunProcess.Id)"
        Stop-Process -Id $GoRunProcess.Id -Force
        $GoRunProcess.WaitForExit(5000) | Out-Null
    }
    foreach ($listenerPid in @(Get-GatewayListenerPids $Address)) {
        if ($listenerPid -and $listenerPid -ne 0) {
            Write-Host "stopping gateway listener pid=$listenerPid"
            Stop-Process -Id $listenerPid -Force -ErrorAction SilentlyContinue
        }
    }
}

function Wait-GatewayReady([string]$Address, [string]$TokenSource, [string]$SessionGroup, [string]$Workspace) {
    $deadline = [DateTimeOffset]::Now.AddSeconds(90)
    while ([DateTimeOffset]::Now -lt $deadline) {
        if (Test-TcpReady $Address) {
            & go run .\examples\workflow-probe -gateway $Address -token-source $TokenSource -session-group $SessionGroup -workspace $Workspace -timeout 10s | Out-Host
            if ($LASTEXITCODE -eq 0) {
                return
            }
        }
        Start-Sleep -Milliseconds 750
    }
    throw "Gateway did not become ready on $Address"
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Resolve-Path -LiteralPath (Join-Path $scriptDir "..\..")
Set-Location $repoRoot

$goCommand = Get-Command go -ErrorAction Stop
$resolvedCodexBinary = Resolve-CodexBinary $CodexBinary
$sourceCodexHome = Resolve-CodexHome $CodexHome
$sourceAuthJson = Join-Path $sourceCodexHome "auth.json"
$authPresent = Test-Path -LiteralPath $sourceAuthJson -PathType Leaf

$workRootFull = Join-Path $repoRoot $WorkRoot
$workspaceDir = Join-Path $workRootFull "workspace"
$workflowStorageDir = Join-Path $workRootFull "workflow-state"
$isolatedCodexHome = Join-Path $workRootFull "codex-home"
$gatewayCodexHome = if ($UseSourceCodexHome) { $sourceCodexHome } else { $isolatedCodexHome }
$tokenSource = Join-Path $workRootFull "gateway.token"
$configPath = Join-Path $workRootFull "gateway.toml"
$transcriptPath = Join-Path $workRootFull "transcript.txt"
$gatewayStdout = Join-Path $workRootFull "gateway.stdout.log"
$gatewayStderr = Join-Path $workRootFull "gateway.stderr.log"

Write-Host "full-e2e real Codex runner"
Write-Host "repo: $repoRoot"
Write-Host "gateway: $Gateway"
Write-Host "codex_binary: $resolvedCodexBinary"
Write-Host "source_codex_home: $sourceCodexHome"
Write-Host ("source_codex_home auth.json: " + $(if ($authPresent) { "present (contents not read)" } else { "missing" }))
Write-Host "gateway_codex_home: $gatewayCodexHome"
Write-Host "codex_model: $CodexModel"
Write-Host "codex_model_provider: $CodexModelProvider"
Write-Host "work_root: $workRootFull"
Write-Host "token_source: <redacted>"
Write-Host "transcript: $transcriptPath"

if ($WhatIfPreference) {
    Write-Host "WhatIf: no token/config files are written and no gateway/e2e process is started"
    return
}

if (-not $authPresent) {
    throw "Codex auth.json was not found under CodexHome. Sign in to Codex or pass the correct -CodexHome."
}

New-Item -ItemType Directory -Force -Path $workRootFull, $workspaceDir, $workflowStorageDir | Out-Null
if (-not $UseSourceCodexHome) {
    $resolvedWorkRootFull = [System.IO.Path]::GetFullPath($workRootFull)
    $resolvedIsolatedCodexHome = [System.IO.Path]::GetFullPath($isolatedCodexHome)
    $requiredPrefix = $resolvedWorkRootFull.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
    if (-not $resolvedIsolatedCodexHome.StartsWith($requiredPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to recreate isolated CodexHome outside work root: $resolvedIsolatedCodexHome"
    }
    if (Test-Path -LiteralPath $isolatedCodexHome) {
        Remove-Item -LiteralPath $isolatedCodexHome -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $isolatedCodexHome | Out-Null
    Copy-Item -LiteralPath $sourceAuthJson -Destination (Join-Path $isolatedCodexHome "auth.json") -Force
    $isolatedConfig = @"
model = $(TomlString $CodexModel)
model_provider = $(TomlString $CodexModelProvider)
"@
    [System.IO.File]::WriteAllText((Join-Path $isolatedCodexHome "config.toml"), $isolatedConfig, [System.Text.UTF8Encoding]::new($false))
    Write-Host "isolated auth.json: copied (contents not printed)"
    Write-Host "isolated config.toml: model/provider only"
}
$token = New-SecretToken
[System.IO.File]::WriteAllText($tokenSource, $token, [System.Text.UTF8Encoding]::new($false))

$config = @"
codex_binary = $(TomlString $resolvedCodexBinary)
listen = $(TomlString $Gateway)
workflow_storage_dir = $(TomlString $workflowStorageDir)
workflow_package_max_bytes = 10485760
workflow_grpc_message_bytes = 12582912
child_env_allowlist = []

[client_auth_token_source]
file = $(TomlString $tokenSource)

[[session_groups]]
session_group_id = $(TomlString $SessionGroup)
workspace_id = $(TomlString $Workspace)
cwd = $(TomlString $workspaceDir)
codex_home = $(TomlString $gatewayCodexHome)

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"

[session_groups.replay_limits]
max_events = 5000
max_bytes = 33554432
ttl_millis = 7200000

[session_groups.thread_binding_limits]
max_bindings = 1000
ttl_millis = 86400000

[session_groups.pending_limits]
max_active_requests = 32
max_display_payload_bytes = 32768
status_non_pending_budget_bytes = 65536

[session_groups.grpc_limits]
inbound_message_bytes = 4194304
outbound_message_bytes = 4194304
"@
[System.IO.File]::WriteAllText($configPath, $config, [System.Text.UTF8Encoding]::new($false))

"full-e2e transcript (token redacted)" | Set-Content -Encoding UTF8 -Path $transcriptPath
"gateway=$Gateway" | Add-Content -Encoding UTF8 -Path $transcriptPath
"codex_binary=$resolvedCodexBinary" | Add-Content -Encoding UTF8 -Path $transcriptPath
"source_codex_home=$sourceCodexHome" | Add-Content -Encoding UTF8 -Path $transcriptPath
"gateway_codex_home=$gatewayCodexHome" | Add-Content -Encoding UTF8 -Path $transcriptPath
"codex_model=$CodexModel" | Add-Content -Encoding UTF8 -Path $transcriptPath
"codex_model_provider=$CodexModelProvider" | Add-Content -Encoding UTF8 -Path $transcriptPath
"token_source=<redacted>" | Add-Content -Encoding UTF8 -Path $transcriptPath

$gatewayProcess = $null
try {
    if ($PSCmdlet.ShouldProcess("codex-runtime-gateway", "start local gateway process")) {
        $existingListeners = @(Get-GatewayListenerPids $Gateway)
        if ($existingListeners.Count -gt 0) {
            throw "Gateway port is already in use on $Gateway by PID(s): $($existingListeners -join ', '). Stop it or pass a different -Gateway."
        }
        $gatewayArgs = @("run", ".\gateway\cmd\codex-runtime-gateway", "--config", $configPath)
        $gatewayProcess = Start-Process -FilePath $goCommand.Source -ArgumentList $gatewayArgs -WorkingDirectory $repoRoot -RedirectStandardOutput $gatewayStdout -RedirectStandardError $gatewayStderr -WindowStyle Hidden -PassThru
        Write-Host "gateway process started: pid=$($gatewayProcess.Id)"
        Wait-GatewayReady $Gateway $tokenSource $SessionGroup $Workspace
    }

    $e2eArgs = @(
        "run", ".\examples\full-e2e",
        "-gateway", $Gateway,
        "-token-source", $tokenSource,
        "-session-group", $SessionGroup,
        "-workspace", $Workspace,
        "-namespace", $Namespace,
        "-workflow-id", $WorkflowId,
        "-workflow-dir", ".\examples\workflows\plain-chat",
        "-mcp-workflow-dir", ".\examples\workflows\writer-notes",
        "-visibility-alpha-workflow-dir", ".\examples\workflows\visibility-alpha",
        "-visibility-beta-workflow-dir", ".\examples\workflows\visibility-beta"
    )
    if ($SkipLegacyTasks) {
        $e2eArgs += "-skip-legacy-tasks"
    }
    if ($SkipMCPNegative) {
        $e2eArgs += "-skip-mcp-negative"
    }
    if ($SkipInterrupt) {
        $e2eArgs += "-skip-interrupt"
    }
    if ($SkipWorkflowVisibility) {
        $e2eArgs += "-skip-workflow-visibility"
    }
    if ($SkipDelete) {
        $e2eArgs += "-skip-delete"
    }
    if ($PSCmdlet.ShouldProcess("examples/full-e2e", "run full SDK/gateway e2e")) {
        $transcriptWriter = [System.IO.StreamWriter]::new($transcriptPath, $true, [System.Text.UTF8Encoding]::new($false))
        try {
            & go @e2eArgs 2>&1 | ForEach-Object {
                $line = $_.ToString()
                Write-Host $line
                $transcriptWriter.WriteLine($line)
            }
        } finally {
            $transcriptWriter.Dispose()
        }
        if ($LASTEXITCODE -ne 0) {
            throw "full-e2e failed with exit code $LASTEXITCODE"
        }
    }
} finally {
    Stop-GatewayProcesses $Gateway $gatewayProcess $KeepGateway.IsPresent
}
