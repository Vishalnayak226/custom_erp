<#
Code map redraw after a release push (item 177.5). The map's file list is exact
in every release commit (G7 files each new file). Its counts - symbols, calls
between areas, busiest functions - come from a code graph that takes minutes to
build on this laptop, so Release.cmd starts this script in the background once
the release commit is pushed. It runs at below-normal priority while GitHub tests.

  pwsh release\scripts\Invoke-BrainRedraw.ps1 -Worktree <release worktree> -Commit <sha>

In the release worktree (the release commit's files) it rebuilds the code graph
(graphify_update.py), redraws docs/brain with update-docs -Group Brain, and
copies the three outputs to %TEMP%\erp_release_brain\. It keeps graphify-out for
the next release's G7 and removes the worktree. Deploy (D6) brings the outputs
into the shared tree for the records commit. status.json there says running,
done or failed; redraw.log has the tool output.
#>
param([Parameter(Mandatory)][string]$Worktree, [Parameter(Mandatory)][string]$Commit)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\ReleaseCommon.ps1"

$out = $BrainRedrawDir
$graphKeep = Join-Path $env:TEMP 'erp_release_graphify'
$statusPath = Join-Path $out 'status.json'
$log = Join-Path $out 'redraw.log'
# The process start time tells this run apart from a later process that reuses its id.
$startTicks = (Get-Process -Id $PID).StartTime.ToFileTimeUtc()
function Set-Status([string]$state, [string]$why = '') {
    # Written whole, then renamed, so a reader never sees half a file.
    $tmp = "$statusPath.tmp"
    [ordered]@{ state = $state; commit = $Commit; pid = $PID; startTicks = $startTicks; at = (Get-Date).ToString('s'); why = $why } |
        ConvertTo-Json | Set-Content -LiteralPath $tmp -Encoding utf8
    Move-Item -LiteralPath $tmp -Destination $statusPath -Force
}

if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Recurse -Force }
New-Item -ItemType Directory -Force -Path $out | Out-Null
Set-Status 'running'
$clock = [Diagnostics.Stopwatch]::StartNew()
try {
    # The laptop's other work comes first (constitution, first principle); the
    # tools below inherit this priority. Two extraction workers at most.
    (Get-Process -Id $PID).PriorityClass = 'BelowNormal'
    $env:GRAPHIFY_MAX_WORKERS = '2'
    $run = Get-GraphifyRun
    $graphOut = Invoke-Native -Exe $run.Exe -Arguments $run.Arguments -WorkDir $Worktree
    $graphOut | Set-Content -LiteralPath $log -Encoding utf8
    $docs = Invoke-Native -Exe 'pwsh' -Arguments @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', 'Brain') -WorkDir $Worktree
    $docs | Add-Content -LiteralPath $log -Encoding utf8
    foreach ($f in $BrainRedrawFiles) {
        $dst = Join-Path $out $f
        New-Item -ItemType Directory -Force -Path (Split-Path $dst -Parent) | Out-Null
        Copy-Item -LiteralPath (Join-Path $Worktree $f) -Destination $dst -Force
    }
    Set-Status 'done' ("{0:N0} s" -f $clock.Elapsed.TotalSeconds)
} catch {
    "$($_.Exception.Message)" | Add-Content -LiteralPath $log -Encoding utf8
    Set-Status 'failed' (($_.Exception.Message -split "`n" | Select-Object -First 3) -join ' ')
} finally {
    $newGraph = Join-Path $Worktree 'graphify-out'
    if (Test-Path -LiteralPath $newGraph) {
        if (Test-Path -LiteralPath $graphKeep) { Remove-Item -LiteralPath $graphKeep -Recurse -Force }
        Move-Item -LiteralPath $newGraph -Destination $graphKeep
    }
    & git -C $RepoRoot worktree remove --force $Worktree 2>$null | Out-Null
    & git -C $RepoRoot worktree prune 2>$null | Out-Null
}
