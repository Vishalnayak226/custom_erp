<#
Generated docs for a records commit (item 232.1). The release notes, the Knowledge
Center and the backlog map are made from docs/project_ledger.md and
docs/micro_checklist.md. A commit of the working records after a release changes
those inputs, and ci.yml's "Generated documentation" check then fails on main until
the next release. Run this after staging the records and before committing them:

  pwsh release\scripts\Update-RecordsDocs.ps1

It regenerates the Content docs in a worktree of HEAD plus the records as staged.
Neither another session's uncommitted code nor its unstaged tracker lines reach the
outputs. It stages each changed output from the worktree and copies it into the
shared tree, except where the shared tree already has its own changes to that file:
that copy is left as is and named. The worktree path stays the same between runs,
so Go's build cache stays warm.
#>
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\ReleaseCommon.ps1"
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$records = @('docs/micro_checklist.md', 'docs/project_ledger.md', 'docs/ai_handover.md')
$wt = Join-Path $env:TEMP 'erp_records_docs'

if (Test-Path -LiteralPath $wt) { Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null }
if (Test-Path -LiteralPath $wt) { Remove-Item -LiteralPath $wt -Recurse -Force }
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'prune') | Out-Null
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'add', '--detach', $wt, 'HEAD') | Out-Null
try {
    foreach ($f in $records) {
        # The staged blob, written by git itself so every byte is kept: only the
        # lines going into the commit count.
        $blob = ("$((Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'ls-files', '-s', '--', $f))[0])" -split '\s+')[1]
        if (-not $blob) { throw "$f is not in the index" }
        Invoke-Native -Exe 'git' -Arguments @('-C', $wt, 'update-index', '--cacheinfo', "100644,$blob,$f") | Out-Null
        Invoke-Native -Exe 'git' -Arguments @('-C', $wt, 'checkout-index', '-f', '--', $f) | Out-Null
    }
    & pwsh -NoProfile -File (Join-Path $wt 'docs\update-docs.ps1') -Group Content
    if ($LASTEXITCODE -ne 0) { throw 'update-docs -Group Content failed in the worktree' }
    $changed = @((& git -C $wt status --porcelain --untracked-files=all) | ForEach-Object { $_.Substring(3).Trim('"') } |
        Where-Object { $_ -and $_ -notin $records })
    foreach ($f in $changed) {
        # Staged straight from the worktree's file, so the commit carries the right
        # output even where another session has its own copy in the shared tree.
        $sha = "$((Invoke-Native -Exe 'git' -Arguments @('-C', $wt, 'hash-object', '-w', '--', $f))[0])".Trim()
        Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'update-index', '--add', '--cacheinfo', "100644,$sha,$f") | Out-Null
        & git -C $repo diff --quiet HEAD -- $f 2>$null
        if ($LASTEXITCODE -ne 0) { Write-Host "  staged; the shared tree's own copy left as is: $f" -ForegroundColor Yellow; continue }
        $dst = Join-Path $repo $f
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $dst) | Out-Null
        Copy-Item -LiteralPath (Join-Path $wt $f) -Destination $dst -Force
    }
    Write-Host $(if ($changed.Count) { "Staged $($changed.Count) generated file(s) with the records:`n  $($changed -join "`n  ")" } else { 'Generated docs already match the records.' })
} finally {
    Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null
}
