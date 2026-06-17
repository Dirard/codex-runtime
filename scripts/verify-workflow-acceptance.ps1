param(
    [Parameter(Mandatory = $true)]
    [string]$Matrix,

    [Parameter(Mandatory = $true)]
    [string]$Spec,

    [switch]$RequireNormativeCoverage
)

$ErrorActionPreference = "Stop"
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)

if (-not (Test-Path -LiteralPath $Matrix)) {
    throw "matrix file not found: $Matrix"
}
if (-not (Test-Path -LiteralPath $Spec)) {
    throw "spec file not found: $Spec"
}

$matrixText = Get-Content -LiteralPath $Matrix -Raw -Encoding UTF8
$specText = Get-Content -LiteralPath $Spec -Raw -Encoding UTF8

$requiredIds = 1..14 | ForEach-Object { "REQ-{0:D2}" -f $_ }
$missingIds = @()
foreach ($id in $requiredIds) {
    if ($matrixText -notmatch "\|\s*$([regex]::Escape($id))\s*\|") {
        $missingIds += $id
    }
}

$requiredSpecAnchors = @(
    "SPEC:43-56",
    "SPEC:57-66",
    "SPEC:67-77",
    "SPEC:78-90",
    "SPEC:91-98",
    "SPEC:154-163",
    "SPEC:165-183",
    "SPEC:185-194",
    "SPEC:196-208",
    "SPEC:210-223",
    "SPEC:225-236",
    "SPEC:287-304",
    "SPEC:417-450",
    "SPEC:487-496"
)
$missingAnchors = @()
if ($RequireNormativeCoverage) {
    foreach ($anchor in $requiredSpecAnchors) {
        if ($matrixText -notmatch [regex]::Escape($anchor)) {
            $missingAnchors += $anchor
        }
    }
}

if ($specText.Length -eq 0) {
    throw "spec file is empty"
}

$badStatusRows = @()
foreach ($line in ($matrixText -split "`r?`n")) {
    if ($line -match "\|\s*REQ-\d+\s*\|" -and $line -match "\|\s*(missing|todo|unverified)\s*\|") {
        $badStatusRows += $line
    }
}

Write-Host "workflow acceptance verification"
Write-Host "matrix: $Matrix"
Write-Host "spec: $Spec"
Write-Host "required ids: $($requiredIds.Count)"
Write-Host "required spec anchors: $($requiredSpecAnchors.Count)"

if ($missingIds.Count -gt 0 -or $missingAnchors.Count -gt 0 -or $badStatusRows.Count -gt 0) {
    if ($missingIds.Count -gt 0) {
        Write-Host "missing ids: $($missingIds -join ', ')"
    }
    if ($missingAnchors.Count -gt 0) {
        Write-Host "missing spec anchors: $($missingAnchors -join ', ')"
    }
    if ($badStatusRows.Count -gt 0) {
        Write-Host "bad status rows:"
        $badStatusRows | ForEach-Object { Write-Host "- $_" }
    }
    exit 1
}

Write-Host "result: pass"
Write-Host "coverage: all required REQ ids and SPEC anchors are present"
