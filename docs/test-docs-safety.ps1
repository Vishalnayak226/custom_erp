# Integration regression: exercise real commands in an isolated temporary copy.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$fixture = Join-Path $tempRoot ('erp-docs-safety-' + [guid]::NewGuid().ToString('N'))
$log = Join-Path $tempRoot ('erp-docs-safety-' + [guid]::NewGuid().ToString('N') + '.log')
function Snapshot {
    $state = [ordered]@{}
    Get-ChildItem -LiteralPath $fixture -Recurse -File | Sort-Object FullName | ForEach-Object {
        $rel = [IO.Path]::GetRelativePath($fixture, $_.FullName)
        $state[$rel] = "$($_.Length):$($_.LastWriteTimeUtc.Ticks):$((Get-FileHash -LiteralPath $_.FullName).Hash)"
    }
    return ($state | ConvertTo-Json -Compress)
}
function Assert-ReadOnly([string]$Exe, [string[]]$CommandArgs, [bool]$MustFail) {
    $before = Snapshot
    & $Exe @CommandArgs *> $log
    $code = $LASTEXITCODE
    if ((Snapshot) -cne $before) { throw "Repository mutation: $Exe $($CommandArgs -join ' ')" }
    if (($MustFail -and $code -eq 0) -or (-not $MustFail -and $code -ne 0)) {
        Get-Content -LiteralPath $log -Tail 25
        throw "Unexpected exit $code for $Exe $($CommandArgs -join ' ')"
    }
    Write-Host "PASS read-only (exit $code): $Exe $($CommandArgs -join ' ')"
}
New-Item -ItemType Directory -Path $fixture | Out-Null
Push-Location $repoRoot
try {
    # Preserve the current working tree, including uncommitted Stage work, without copying secrets/builds.
    $files = @(& git ls-files --cached --others --exclude-standard) + @('graphify-out/graph.json')
    foreach ($rel in $files | Sort-Object -Unique) {
        if (-not (Test-Path -LiteralPath $rel -PathType Leaf)) { continue }
        if ($rel -match '\.local\.|(^|/)CLAUDE\.md$') { continue }
        $target = Join-Path $fixture $rel
        New-Item -ItemType Directory -Path (Split-Path -Parent $target) -Force | Out-Null
        Copy-Item -LiteralPath $rel -Destination $target
    }
    # The brain's file inventory includes ignored agent guidance on the source workstation.
    # Content checks below are portable. Brain failure purity is tested against the fixture graph.
    Set-Location $fixture
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'Content', '-Check') $false
    Assert-ReadOnly 'go' @('run', './cmd/gendocs', '-check') $false
    Assert-ReadOnly 'go' @('run', './cmd/genkb', '-check') $false
    foreach ($rel in @('docs/guides/ERROR_CODES.md', 'internal/kb/content/articles/report-catalog.html', 'docs/brain/BRAIN.md')) {
        Add-Content -LiteralPath $rel -Value 'Intentional drift fixture.'
    }
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/guides/update-guides.ps1', '-Check') $true
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/kb/update-kb.ps1', '-Check') $true
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/brain/update-brain.ps1', '-Check') $true
    Assert-ReadOnly 'go' @('run', './cmd/gendocs', '-check') $true
    Assert-ReadOnly 'go' @('run', './cmd/genkb', '-check') $true
    Assert-ReadOnly 'go' @('run', './cmd/brainmap', '-check') $true
    Set-Content -LiteralPath 'internal/kb/content/articles/orphan-fixture.html' -Value 'orphan'
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'Content') $true
    Remove-Item -LiteralPath 'internal/kb/content/articles/orphan-fixture.html'
    # A missing mandatory input must fail before any generated output is published.
    Remove-Item -LiteralPath 'docs/project_ledger.md'
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'Content') $true
    Write-Host 'Documentation safety integration checks passed.'
} finally {
    Pop-Location
    $resolved = [IO.Path]::GetFullPath($fixture)
    if ($resolved.StartsWith($tempRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path -Leaf $resolved) -like 'erp-docs-safety-*') {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
    if (Test-Path -LiteralPath $log) { Remove-Item -LiteralPath $log }
}
