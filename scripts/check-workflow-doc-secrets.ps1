param()

$ErrorActionPreference = "Stop"
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$scanRoots = @(
    "README.md",
    "docs",
    "examples/api-handler",
    "examples/e2e-chat",
    "examples/full-e2e",
    "examples/workflow-probe",
    "examples/workflow-scaffold",
    "examples/workflow-smoke",
    "examples/workflows"
)
$extensions = @(".md", ".ps1", ".toml", ".go")
$rules = @(
    @{ Name = "OpenAI secret key literal"; Pattern = "(?<![A-Za-z0-9])sk-[A-Za-z0-9_-]{20,}" },
    @{ Name = "private key block"; Pattern = "-----BEGIN (RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----" },
    @{ Name = "raw bearer header"; Pattern = "(?i)authorization\s*:\s*bearer\s+(?!<|redacted|\[redacted\])\S+" },
    @{ Name = "raw token env assignment"; Pattern = "(?m)CODEX_RUNTIME(?:_[A-Z0-9]+)*_TOKEN\s*=\s*(?!<|redacted|\[redacted\])\S+" },
    @{ Name = "raw cookie header"; Pattern = "(?i)(cookie|set-cookie)\s*:\s*(?!<|redacted|\[redacted\])\S+" },
    @{ Name = "raw app-server jsonl"; Pattern = "(?i)\{.*(""jsonrpc""|""method"").*\}" }
)

function Add-ScanFile {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        return @()
    }
    $item = Get-Item -LiteralPath $Path
    if (-not $item.PSIsContainer) {
        if ($extensions -contains $item.Extension) { return @($item) }
        return @()
    }
    return @(Get-ChildItem -LiteralPath $item.FullName -Recurse -File | Where-Object { $extensions -contains $_.Extension })
}

$files = @()
foreach ($root in $scanRoots) {
    $files += Add-ScanFile -Path (Join-Path $repoRoot $root)
}

$violations = @()
foreach ($file in $files) {
    $text = Get-Content -LiteralPath $file.FullName -Raw -Encoding UTF8
    foreach ($rule in $rules) {
        if ($text -match $rule.Pattern) {
            $relative = Resolve-Path -LiteralPath $file.FullName -Relative
            $violations += "$relative :: $($rule.Name)"
        }
    }
}

Write-Host "workflow doc secret check"
Write-Host "files scanned: $($files.Count)"

if ($violations.Count -gt 0) {
    Write-Host "violations:"
    $violations | Sort-Object | ForEach-Object { Write-Host "- $_" }
    exit 1
}

Write-Host "result: pass"
Write-Host "raw bearer tokens/auth headers/secret literals/app-server JSONL: none"
