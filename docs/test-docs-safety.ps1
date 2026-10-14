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
# $MustFail: $true / $false asserts the verdict; $null asserts only that
# nothing was written. $Reason, when given, must appear in the output, so a
# must-fail case cannot pass by failing for some other reason.
function Assert-ReadOnly([string]$Exe, [string[]]$CommandArgs, $MustFail, [string]$Reason = '') {
    $before = Snapshot
    & $Exe @CommandArgs *> $log
    $code = $LASTEXITCODE
    if ((Snapshot) -cne $before) { throw "Repository mutation: $Exe $($CommandArgs -join ' ')" }
    if ($null -ne $MustFail -and (($MustFail -and $code -eq 0) -or (-not $MustFail -and $code -ne 0))) {
        Get-Content -LiteralPath $log -Tail 25
        throw "Unexpected exit $code for $Exe $($CommandArgs -join ' ')"
    }
    if ($Reason -and -not (Select-String -LiteralPath $log -SimpleMatch -Quiet -Pattern $Reason)) {
        Get-Content -LiteralPath $log -Tail 25
        throw "Failed for another reason than '$Reason': $Exe $($CommandArgs -join ' ')"
    }
    $verdict = if ($null -eq $MustFail) { ', verdict not asserted' } else { '' }
    Write-Host "PASS read-only (exit $code$verdict): $Exe $($CommandArgs -join ' ')"
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
    # 47.15.1: publish into the fixture first, so the pass cases below test
    # that each check only reads, not how current the generated files of the
    # tree this runs in happen to be. -Group KB runs gendocs, genkb and the
    # manuals without doclint, whose verdict depends on governance review
    # dates rather than on generator safety.
    & pwsh -NoProfile -File docs/update-docs.ps1 -Group KB *> $log
    if ($LASTEXITCODE -ne 0) { Get-Content -LiteralPath $log -Tail 25; throw 'Could not publish generated outputs into the fixture' }
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'KB', '-Check') $false
    Assert-ReadOnly 'go' @('run', './cmd/gendocs', '-check') $false
    Assert-ReadOnly 'go' @('run', './cmd/genkb', '-check') $false
    $release = (Get-Content -LiteralPath 'internal/server/VERSION' -Raw).Trim()
    Assert-ReadOnly 'go' @('run', './cmd/genkb', '-manuals', 'docs/governance/manual-selection.json', '-release', $release, '-out', 'docs/user', '-check') $false
    # gendocs never writes without an explicit staging root.
    Assert-ReadOnly 'go' @('run', './cmd/gendocs') $true
    # surfacescan: -check reads only, and -out to %TEMP% must keep working,
    # because Controlled Folder Access and the agent permission classifier
    # both refuse an in-place overwrite of docs/security/attack_surface.json.
    $surface = Join-Path $tempRoot ('erp-docs-safety-surface-' + [guid]::NewGuid().ToString('N') + '.json')
    try {
        Assert-ReadOnly 'go' @('run', './cmd/surfacescan', '-out', $surface) $false
        if (-not (Test-Path -LiteralPath $surface)) { throw 'surfacescan -out did not write the %TEMP% inventory' }
        Assert-ReadOnly 'go' @('run', './cmd/surfacescan', '-check', '-out', $surface) $false
        Copy-Item -LiteralPath $surface -Destination 'docs/security/attack_surface.json' -Force
        Assert-ReadOnly 'go' @('run', './cmd/surfacescan', '-check') $false
    } finally {
        if (Test-Path -LiteralPath $surface) { Remove-Item -LiteralPath $surface }
    }
    # Every check entry point reads only, whatever its verdict (governance
    # review dates, the workstation's brain inventory).
    foreach ($script in @('docs/update-docs.ps1', 'docs/guides/update-guides.ps1', 'docs/brain/update-brain.ps1')) {
        Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', $script, '-Check') $null
    }
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'Content', '-Check') $null
    Assert-ReadOnly 'go' @('run', './cmd/brainmap', '-check') $null
    foreach ($rel in @('docs/guides/ERROR_CODES.md', 'internal/kb/content/articles/report-catalog.html', 'docs/brain/BRAIN.md', 'docs/user/user-manual.html', 'docs/security/attack_surface.json')) {
        Add-Content -LiteralPath $rel -Value 'Intentional drift fixture.'
    }
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/guides/update-guides.ps1', '-Check') $true
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/kb/update-kb.ps1', '-Check') $true
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/brain/update-brain.ps1', '-Check') $true
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Check') $true
    Assert-ReadOnly 'go' @('run', './cmd/gendocs', '-check') $true
    Assert-ReadOnly 'go' @('run', './cmd/genkb', '-check') $true
    Assert-ReadOnly 'go' @('run', './cmd/genkb', '-manuals', 'docs/governance/manual-selection.json', '-release', $release, '-out', 'docs/user', '-check') $true
    Assert-ReadOnly 'go' @('run', './cmd/brainmap', '-check') $true
    Assert-ReadOnly 'go' @('run', './cmd/surfacescan', '-check') $true
    # Restore the KB drift so the next two fail for their own reason.
    & pwsh -NoProfile -File docs/update-docs.ps1 -Group KB *> $log
    if ($LASTEXITCODE -ne 0) { Get-Content -LiteralPath $log -Tail 25; throw 'Could not restore the fixture' }
    Set-Content -LiteralPath 'internal/kb/content/articles/orphan-fixture.html' -Value 'orphan'
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'KB') $true 'unregistered generated output'
    Remove-Item -LiteralPath 'internal/kb/content/articles/orphan-fixture.html'
    # A missing mandatory input must fail before any generated output is published.
    Remove-Item -LiteralPath 'docs/project_ledger.md'
    Assert-ReadOnly 'pwsh' @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'KB') $true 'project_ledger.md'
    Write-Host 'Documentation safety integration checks passed.'
} finally {
    Pop-Location
    $resolved = [IO.Path]::GetFullPath($fixture)
    if ($resolved.StartsWith($tempRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path -Leaf $resolved) -like 'erp-docs-safety-*') {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
    if (Test-Path -LiteralPath $log) { Remove-Item -LiteralPath $log }
}
