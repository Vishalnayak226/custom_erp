<#
Generated docs for a records commit (item 232.1). The release notes, the Knowledge
Center and the backlog map are made from docs/project_ledger.md and
docs/micro_checklist.md. A commit of the working records after a release changes
those inputs, and ci.yml's "Generated documentation" check then fails on main until
the next release. Run this after staging the records and before committing them:

  pwsh release\scripts\Update-RecordsDocs.ps1

It regenerates the Content docs in a worktree of HEAD plus the records (and any
other file) as staged.
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
# 265.9: every staged file goes into the worktree too, not only the records: a records
# commit may carry the fix a generator needs (0.29.0's ledger section pushed the Help
# search index over budget, and the fix was in internal/kb).
$staged = @(@($records) + @(& git -C $repo diff --cached --name-only --diff-filter=AM) | Where-Object { $_ } | Select-Object -Unique)
$wt = Join-Path $env:TEMP 'erp_records_docs'

if (Test-Path -LiteralPath $wt) { Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null }
if (Test-Path -LiteralPath $wt) { Remove-Item -LiteralPath $wt -Recurse -Force }
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'prune') | Out-Null
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'add', '--detach', $wt, 'HEAD') | Out-Null
try {
    foreach ($f in $staged) {
        # The staged blob, written by git itself so every byte is kept: only the
        # lines going into the commit count.
        $blob = ("$((Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'ls-files', '-s', '--', $f))[0])" -split '\s+')[1]
        if (-not $blob) { throw "$f is not in the index" }
        Invoke-Native -Exe 'git' -Arguments @('-C', $wt, 'update-index', '--cacheinfo', "100644,$blob,$f") | Out-Null
        Invoke-Native -Exe 'git' -Arguments @('-C', $wt, 'checkout-index', '-f', '--', $f) | Out-Null
    }
    & pwsh -NoProfile -File (Join-Path $wt 'docs\update-docs.ps1') -Group Content
    if ($LASTEXITCODE -ne 0) { throw 'update-docs -Group Content failed in the worktree' }
    # What the generators changed against the staged files (the worktree's index), so an
    # output staged by an earlier run is made again rather than taken as an input.
    $changed = @(@(& git -C $wt diff --name-only) + @(& git -C $wt ls-files --others --exclude-standard) |
        ForEach-Object { "$_".Trim('"') } | Where-Object { $_ -and $_ -notin $records } | Select-Object -Unique)
    foreach ($f in $changed) {
        # Staged straight from the worktree's file, so the commit carries the right
        # output even where another session has its own copy in the shared tree.
        $sha = "$((Invoke-Native -Exe 'git' -Arguments @('-C', $wt, 'hash-object', '-w', '--', $f))[0])".Trim()
        # The shared tree's copy is another session's when it differs from what is staged
        # (HEAD's file, or an earlier run's output), so check before staging the new one.
        & git -C $repo diff --quiet -- $f 2>$null
        $theirs = $LASTEXITCODE -ne 0
        Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'update-index', '--add', '--cacheinfo', "100644,$sha,$f") | Out-Null
        if ($theirs) { Write-Host "  staged; the shared tree's own copy left as is: $f" -ForegroundColor Yellow; continue }
        $dst = Join-Path $repo $f
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $dst) | Out-Null
        Copy-Item -LiteralPath (Join-Path $wt $f) -Destination $dst -Force
    }
    Write-Host $(if ($changed.Count) { "Staged $($changed.Count) generated file(s) with the records:`n  $($changed -join "`n  ")" } else { 'Generated docs already match the records.' })
} finally {
    Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null
}
