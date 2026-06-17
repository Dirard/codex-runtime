param()

$ErrorActionPreference = "Stop"
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$examplesRoot = Join-Path $repoRoot "examples"
$candidateExtensions = @(".js", ".jsx", ".ts", ".tsx", ".html", ".css", ".vue", ".svelte")
$candidateNamePattern = "\\(browser|client|web|frontend)\\"
$forbiddenPatterns = @(
    @{ Name = "gateway token env"; Pattern = "CODEX_RUNTIME_TOKEN(_SOURCE)?" },
    @{ Name = "auth header"; Pattern = "(?i)authorization\s*:\s*bearer|(?i)Authorization\s*:\s*Bearer" },
    @{ Name = "loopback gateway address"; Pattern = "127\.0\.0\.1:5575|localhost:5575|\[::1\]:5575" },
    @{ Name = "grpc gateway dial"; Pattern = "grpc\.(Dial|NewClient)|@grpc|grpc-web" }
)

$candidateFiles = @()
if (Test-Path -LiteralPath $examplesRoot) {
    $candidateFiles = Get-ChildItem -LiteralPath $examplesRoot -Recurse -File |
        Where-Object {
            $candidateExtensions -contains $_.Extension -and
            ($_.FullName -replace [regex]::Escape($repoRoot), "") -match $candidateNamePattern
        }
}

$violations = @()
foreach ($file in $candidateFiles) {
    $text = Get-Content -LiteralPath $file.FullName -Raw -Encoding UTF8
    foreach ($rule in $forbiddenPatterns) {
        if ($text -match $rule.Pattern) {
            $relative = Resolve-Path -LiteralPath $file.FullName -Relative
            $violations += "$relative :: $($rule.Name)"
        }
    }
}

Write-Host "workflow browser boundary check"
Write-Host "browser/client candidate files: $($candidateFiles.Count)"
Write-Host "server-side gateway examples allowed: examples/api-handler, examples/workflow-probe, examples/workflow-smoke"

if ($violations.Count -gt 0) {
    Write-Host "violations:"
    $violations | Sort-Object | ForEach-Object { Write-Host "- $_" }
    exit 1
}

Write-Host "result: pass"
Write-Host "gateway credentials/direct gateway calls in browser/client examples: none"
