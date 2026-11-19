<#
Release click (Stage 109.1): every gate a release needs before origin/main
moves - the routine the 2026-10-18/19 releases (840909b, 0.2.0-0.4.0) ran by
hand, in the same order. It commits and pushes only when every gate passes.

  release\Release.cmd                                    # gates, commit, push
  pwsh release\scripts\Invoke-Release.ps1 -DryRun         # gates only; nothing committed
  pwsh release\scripts\Invoke-Release.ps1 -NoPush         # gates + local commit
  pwsh release\scripts\Invoke-Release.ps1 -QuietMinutes 0 # skip the "tree is quiet" wait
  pwsh release\scripts\Invoke-Release.ps1 -Wait           # also wait for the GitHub run
  pwsh release\scripts\Invoke-Release.ps1 -LocalTests     # build and test on this laptop

Where the tests run (item 177.1, user 2026-11-14): on GitHub's machines. The
click commits the release and pushes it to candidate/<version>. The Release
workflow (.github/workflows/release.yml) builds that commit, runs the whole
suite in 16 shards, and moves main to it only when every job passes; Deploy.cmd
waits for that. -LocalTests (or config.json "testsOn": "local") runs G8 and G10
here instead and pushes main itself, as before.

What ships: everything modified or new in the working tree, minus the paths
in release\hold.txt (git-ignored; one path or glob per line, `#` starts
a comment). A session whose work must not ship yet lists its paths there.

A release problem this script did not catch is a missing gate: add it here and
to the gate table in release\README.md in the same session (CLAUDE.md rule).
#>
param(
    [switch]$DryRun,
    [switch]$NoPush,
    [int]$QuietMinutes = -1,
    [switch]$KeepWorktree,
    [switch]$LocalTests,
    [switch]$Wait
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\ReleaseCommon.ps1"

$cfg = Get-ReleaseConfig
$onGitHub = -not $LocalTests -and "$($cfg.testsOn)" -ne 'local'
$repo = $RepoRoot
$go = $cfg.goBin
$goDir = Split-Path $go -Parent
$gofmt = Join-Path $goDir 'gofmt.exe'
if ($QuietMinutes -lt 0) { $QuietMinutes = [int]$cfg.quietMinutes }
$ts = Get-Date -Format 'yyyyMMdd_HHmmss'
$reportDir = Join-Path $ReleaseDir 'reports'
New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
$reportPath = Join-Path $reportDir "release-$ts.md"
$report = [System.Collections.Generic.List[string]]::new()
$report.Add("# Release $ts")
# One fixed folder, not one per run (177.5): Go keys its build cache by a
# package's folder, so a new folder per run compiled every tool from cold
# (G4b alone was 130 s). G0a lets one release run at a time; G1 clears a
# folder a dead run left behind.
$wt = Join-Path $env:TEMP 'erp_release'
$graphKeep = Join-Path $env:TEMP 'erp_release_graphify'   # graphify-out between runs (G7)
$scratchDb = "custom_erp_release_$ts"
$state = [ordered]@{ status = 'failed'; at = (Get-Date).ToString('o'); commit = ''; tree = ''; findings = @(); flaky = @(); held = @() }

function Note([string]$line) { Write-Host "   $line"; $report.Add("- $line") }
function Save-Report {
    $report | Set-Content -LiteralPath $reportPath -Encoding utf8
    $state | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $reportDir 'last-release.json') -Encoding utf8
}
function Stop-Release([string]$why) {
    Write-Host ''
    Write-Host "RELEASE STOPPED: $why" -ForegroundColor Red
    # 177.5: a stop mid-G7 leaves G4c's background build running; end it and its compilers.
    if ($script:budgetBuild -and -not $script:budgetBuild.Proc.HasExited) { & taskkill.exe /T /F /PID $script:budgetBuild.Proc.Id *> $null }
    $report.Add(''); $report.Add("**Stopped:** $why")
    if (Test-Path $wt) { $report.Add("Worktree kept for inspection: $wt") }
    Save-Report
    Write-Host "Report: $reportPath"
    exit 1
}
function Invoke-Git([string[]]$a, [string]$dir = $repo, [switch]$AllowFail) { Invoke-Native -Exe 'git' -Arguments (@('-C', $dir) + $a) -AllowFail:$AllowFail }
function Get-ContentHash([string]$p) { if (Test-Path -LiteralPath $p -PathType Leaf) { (Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash } else { 'MISSING' } }
function Test-Binary([string]$p) {
    $fs = [IO.File]::OpenRead($p)
    try { $buf = New-Object byte[] 8000; $n = $fs.Read($buf, 0, 8000) } finally { $fs.Dispose() }
    for ($i = 0; $i -lt $n; $i++) { if ($buf[$i] -eq 0) { return $true } }
    return $false
}
function Read-Text([string]$p) { [IO.File]::ReadAllText($p) }
function Write-Text([string]$p, [string]$s) { [IO.File]::WriteAllText($p, $s, [Text.UTF8Encoding]::new($false)) }
function Invoke-Docs([string]$group) {
    $out = Invoke-Native -Exe 'pwsh' -Arguments @('-NoProfile', '-File', 'docs/update-docs.ps1', '-Group', $group) -WorkDir $wt -AllowFail
    return @{ ok = ($LASTEXITCODE -eq 0); out = $out }
}

# G0a One release at a time (2026-10-26: two queued runs went G0-G10 side by side and each
# stopped at G11 on the other's git index.lock). CreateNew is atomic; a lock whose process is
# gone (or is a different process with a reused PID) was left by a run that died, and is taken over.
$lockPath = Join-Path $reportDir 'release.lock'
$lockHeld = $false
for ($attempt = 0; -not $lockHeld -and $attempt -lt 2; $attempt++) {
    try {
        $fs = [IO.File]::Open($lockPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
        $bytes = [Text.Encoding]::UTF8.GetBytes((@{ pid = $PID; started = (Get-Process -Id $PID).StartTime.ToString('o') } | ConvertTo-Json -Compress))
        $fs.Write($bytes, 0, $bytes.Length); $fs.Dispose()
        $lockHeld = $true
    } catch {
        if (-not (Test-Path -LiteralPath $lockPath)) { throw }
        $held = $null; try { $held = Get-Content -LiteralPath $lockPath -Raw | ConvertFrom-Json } catch { }
        $owner = if ($held -and $held.pid) { Get-Process -Id $held.pid -ErrorAction SilentlyContinue } else { $null }
        if ($owner -and [math]::Abs(($owner.StartTime - [datetime]$held.started).TotalSeconds) -lt 5) {
            # Not Stop-Release: its Save-Report would overwrite the running release's last-release.json.
            Write-Host "RELEASE STOPPED: another release is running (pid $($held.pid), started $($owner.StartTime.ToString('HH:mm'))) - let it finish, then run Release again" -ForegroundColor Red
            exit 1
        }
        Remove-Item -LiteralPath $lockPath -Force -ErrorAction SilentlyContinue
    }
}
if (-not $lockHeld) { Stop-Release "could not take the release lock $lockPath" }

try {
# ---------------------------------------------------------------- G0 preflight
Write-Gate 'G0 Preflight'
foreach ($tool in @('git', 'node', 'pwsh', 'graphify')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Stop-Release "$tool is not on PATH" }
}
foreach ($p in @($go, $gofmt, $cfg.psql)) { if (-not (Test-Path $p)) { Stop-Release "not found: $p (release\config.json)" } }
if ($onGitHub) {
    # 177.1: the tests run on GitHub, so the push to candidate/<version> and the wait need gh signed in.
    if (-not (Get-Command 'gh' -ErrorAction SilentlyContinue)) { Stop-Release 'gh (GitHub CLI) is not on PATH - install it, or run with -LocalTests' }
    & gh auth status 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Stop-Release 'gh is not signed in - run "gh auth login", or run with -LocalTests' }
    # 204.1: GitHub's Linux runner has no TEMP or USERPROFILE. 0.25.0's run died loading
    # ReleaseCommon.ps1 there, before any test ran. Load it here the same way.
    $probe = "`$ErrorActionPreference = 'Stop'; Remove-Item Env:TEMP, Env:TMP, Env:USERPROFILE -ErrorAction SilentlyContinue; . '$PSScriptRoot/ReleaseCommon.ps1'"
    $probeOut = & pwsh -NoProfile -NonInteractive -Command $probe 2>&1
    if ($LASTEXITCODE -ne 0) { Stop-Release "release/scripts/ReleaseCommon.ps1 does not load without TEMP/USERPROFILE, as on GitHub's runner: $(($probeOut | Out-String).Trim())" }
    Note 'release scripts load as on GitHub''s runner (no TEMP)'
    # 204.1: 0.25.0's promote job passed every test and still could not move main: it
    # ran `git push` from actions/checkout's depth-1 clone, which never saw main's tip
    # ("fetch first"). Trial runs never reach promote, so only a real release finds this.
    $promote = ((Get-Content -Raw (Join-Path $repo '.github/workflows/release.yml')) -split '(?m)^  promote:', 2)[1]
    if (-not $promote) { Stop-Release '.github/workflows/release.yml has no promote job - nothing would move main' }
    if ($promote -match 'git push[^\r\n]*refs/heads/main' -and $promote -notmatch 'fetch-depth:\s*0') {
        Stop-Release '.github/workflows/release.yml promote pushes main from a depth-1 checkout, which GitHub refuses ("fetch first") - move main through the API (gh api -X PATCH .../git/refs/heads/main -F force=false) or check out with fetch-depth: 0'
    }
    # 47.9.4/47.17.2 (ask 241): G10b defers browser budgets and journeys to release.yml's
    # evidence job, so promote must wait for that job or main moves without them.
    if ($promote -notmatch '(?m)^\s+needs:[^\r\n]*\bevidence\b') {
        Stop-Release '.github/workflows/release.yml promote does not need the evidence job - G10b defers browser budgets and journeys to it, so main would move without them'
    }
    Note 'release.yml promote can move main and waits for the evidence job'
}
$branch =(Invoke-Git @('rev-parse', '--abbrev-ref', 'HEAD'))[0]
if ($branch -ne 'main') { Stop-Release "the repo is on '$branch', not main" }
$staged = @((Invoke-Git @('diff', '--cached', '--name-only')) | Where-Object { $_ })
if ($staged.Count) {
    # 2026-10-19: a finished session had left a `git rm` staged; unstaging kept the deletion and Release shipped it.
    Stop-Release "already staged in the index: $(($staged | Select-Object -First 6) -join ', ') - a session may be mid-commit; let it finish. If it is finished work, ``git reset -q -- <path>`` unstages it and keeps the change; then run Release again"
}
Invoke-Git @('fetch', '-q', 'origin') | Out-Null
$behind = [int](Invoke-Git @('rev-list', '--count', 'main..origin/main'))[0]
if ($behind -gt 0) { Stop-Release "main is $behind commit(s) behind origin/main - pull first" }
$startHead = (Invoke-Git @('rev-parse', 'HEAD'))[0]
Note "HEAD $($startHead.Substring(0,7)) on main, not behind origin"
$pgArgs = @('-w', '-h', $cfg.pgHost, '-p', "$($cfg.pgPort)", '-U', $cfg.pgUser, '-d', 'postgres', '-tAc')
Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + 'select 1') | Out-Null
Note "Postgres on $($cfg.pgHost):$($cfg.pgPort) answers"

# What the tree holds: " M"/"??" add or change a file, " D" deletes one.
$raw = (& git -C $repo status --porcelain=v1 -z -uall) -join ''
$entries = $raw -split "`0" | Where-Object { $_ }
$changed = [System.Collections.Generic.List[object]]::new()
foreach ($e in $entries) { $changed.Add([pscustomobject]@{ Code = $e.Substring(0, 2); Path = $e.Substring(3) }) }

$holdFile = Join-Path $ReleaseDir 'hold.txt'
$holds = @()
if (Test-Path $holdFile) {
    $holds = Get-Content $holdFile | ForEach-Object { ($_ -replace '#.*$', '').Trim() } | Where-Object { $_ }
}
# A docs run made with held work in the tree lists that work's outputs in these
# manifests, and G7's orphan check then stops the release (2026-10-29: Stage
# 140.12's report-catalog-4 page). G7 regenerates all three from the release
# set, so with anything held the snapshot starts them from HEAD.
if (@($holds).Count) { $holds = @($holds) + @('docs/generated/guides-manifest.json', 'docs/generated/kb-manifest.json', 'docs/generated/manuals-manifest.json') }
function Test-Held([string]$path) {
    foreach ($h in $holds) {
        $pat = $h.Replace('\', '/')
        if ($pat.EndsWith('/')) { if ($path.StartsWith($pat)) { return $true } }
        elseif ($path -like $pat) { return $true }
    }
    return $false
}
$ship = @($changed | Where-Object { -not (Test-Held $_.Path) })
$held = @($changed | Where-Object { Test-Held $_.Path } | ForEach-Object Path)
$state.held = $held
if ($held.Count) { Note "held back ($($held.Count)): $($held -join ', ')" }

if ($QuietMinutes -gt 0) {
    $cut = (Get-Date).AddMinutes(-$QuietMinutes)
    $busy = @($ship | Where-Object { $_.Code -ne ' D' } | Where-Object {
        $f = Join-Path $repo $_.Path; (Test-Path -LiteralPath $f) -and ((Get-Item -LiteralPath $f).LastWriteTime -gt $cut) } | ForEach-Object Path)
    if ($busy.Count) { Stop-Release "files changed in the last $QuietMinutes min - a session may still be working: $(($busy | Select-Object -First 8) -join ', ')" }
    Note "tree quiet for $QuietMinutes min"
}
$ahead = [int](Invoke-Git @('rev-list', '--count', 'origin/main..main'))[0]
if ($ship.Count -eq 0 -and $ahead -eq 0) {
    Write-Host 'Nothing to release: the tree matches origin/main.' -ForegroundColor Green
    $state.status = 'nothing'; $state.commit = $startHead; Save-Report; exit 0
}
Note "$($ship.Count) file(s) to ship, $ahead unpushed commit(s)"

# ----------------------------------------------------------------- G1 snapshot
# A clean worktree at HEAD plus the shipped files: the release is built and
# tested exactly as it will be committed, while other sessions keep working in
# the shared tree. LF checkout, so it matches the commit byte for byte.
Write-Gate 'G1 Snapshot'
# 177.5: the last release's code map redraw works in this folder until it ends.
$redraw = Read-BrainRedraw
if ($redraw -and $redraw.state -eq 'running') {
    Note "waiting for the last release's code map redraw (pid $($redraw.pid))"
    $redraw = Wait-BrainRedraw 10
    if ($redraw.state -eq 'running') { Stop-Release "the code map redraw (pid $($redraw.pid)) is still running after 10 min - see $BrainRedrawDir\redraw.log" }
}
if (Test-Path -LiteralPath $wt) {
    # A run that died (or -KeepWorktree) left it; keep its graph cache, drop the rest.
    $oldGraph = Join-Path $wt 'graphify-out'
    if ((Test-Path -LiteralPath $oldGraph) -and -not (Test-Path -LiteralPath $graphKeep)) { Move-Item -LiteralPath $oldGraph -Destination $graphKeep }
    Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null
    if (Test-Path -LiteralPath $wt) { Remove-Item -LiteralPath $wt -Recurse -Force }
}
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'prune') -AllowFail | Out-Null
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, '-c', 'core.autocrlf=false', 'worktree', 'add', '--detach', $wt, 'HEAD') | Out-Null
$snapHash = @{}
foreach ($c in $ship) {
    $src = Join-Path $repo $c.Path
    $dst = Join-Path $wt $c.Path
    if ($c.Code -eq ' D') {
        if (Test-Path -LiteralPath $dst) { Remove-Item -LiteralPath $dst -Force }
        $snapHash[$c.Path] = 'MISSING'
        continue
    }
    New-Item -ItemType Directory -Force -Path (Split-Path $dst -Parent) | Out-Null
    Copy-Item -LiteralPath $src -Destination $dst -Force
    $snapHash[$c.Path] = Get-ContentHash $src
}
Note "worktree $wt"

# ------------------------------------------------------------- G2 line endings
# git apply / some editors write CRLF under core.autocrlf=true; the doc
# generators hash bytes, so they must see what will be committed. Never a
# binary (four PNGs were nearly corrupted this way on 2026-10-19), never a file
# the index already stores with CRLF (the docs/generated manifests).
Write-Gate 'G2 Line endings'
$mixed = @((Invoke-Git @('ls-files', '--eol') $wt) | Where-Object { $_ -match '^i/(mixed|crlf)' } | ForEach-Object { ($_ -split "`t")[-1] })
$fixed = 0
# Files .gitattributes keeps CRLF (the .cmd launchers) stay as they are. One
# check-attr per 200 paths, not one per file (177.5: 502 files took 71 s).
$keepCrlf = @{}
$present = @($ship | Where-Object { $_.Code -ne ' D' } | ForEach-Object { $_.Path })
for ($i = 0; $i -lt $present.Count; $i += 200) {
    $chunk = $present[$i..([Math]::Min($i + 199, $present.Count - 1))]
    & git -C $wt check-attr eol -- @chunk | ForEach-Object { if ($_ -match '^(.*): eol: crlf$') { $keepCrlf[$Matches[1]] = $true } }
}
foreach ($c in $ship | Where-Object { $_.Code -ne ' D' }) {
    $p = Join-Path $wt $c.Path
    if ($mixed -contains $c.Path -or $keepCrlf.ContainsKey($c.Path) -or (Test-Binary $p)) { continue }
    $s = Read-Text $p
    if ($s.Contains("`r`n")) { Write-Text $p ($s.Replace("`r`n", "`n")); $fixed++ }
}
Note "$fixed file(s) converted to LF"

# ---------------------------------------------------------------- G3 gofmt
Write-Gate 'G3 gofmt'
# One gofmt -l over every shipped Go file (177.5); only the ones it names are
# looked at one by one.
$goShip = @($ship | Where-Object { $_.Code -ne ' D' -and $_.Path -like '*.go' })
$unformatted = @{}
for ($i = 0; $i -lt $goShip.Count; $i += 200) {
    $chunk = @($goShip[$i..([Math]::Min($i + 199, $goShip.Count - 1))] | ForEach-Object { $_.Path })
    Invoke-Native -Exe $gofmt -Arguments (@('-l') + $chunk) -WorkDir $wt -AllowFail | Where-Object { $_ } | ForEach-Object { $unformatted[$_.Trim().Replace('\', '/')] = $true }
}
foreach ($c in $goShip | Where-Object { $unformatted.ContainsKey($_.Path) }) {
    $p = Join-Path $wt $c.Path
    $headWasClean = $true
    if ($c.Code -ne '??') {
        # HEAD's copy goes through a file: a PowerShell pipe would add CRLF and
        # make every file look unformatted.
        $headTmp = Join-Path $env:TEMP "release_gofmt_$ts.go"
        Write-Text $headTmp (((& git -C $wt show "HEAD:$($c.Path)") -join "`n") + "`n")
        $headWasClean = -not ((Invoke-Native -Exe $gofmt -Arguments @('-l', $headTmp) -AllowFail) | Where-Object { $_ })
        Remove-Item $headTmp -ErrorAction SilentlyContinue
    }
    if ($headWasClean) { Invoke-Native -Exe $gofmt -Arguments @('-w', $p) | Out-Null; Note "gofmt -w $($c.Path)" }
}

# ---------------------------------------------------------- G4 cache versions
# Browsers keep yesterday's screens unless the ?v= numbers move (Stage 53.17):
# any changed file under public/ bumps the view-module version (and app.js?v=,
# which TestStage55ShellAndViewModuleCacheVersionsAgree keeps equal); a changed
# styles.css bumps its own ?v=.
Write-Gate 'G4 Cache versions'
$publicChanged = @($ship | Where-Object { $_.Path -like 'public/*' -and $_.Path -ne 'public/index.html' })
if ($publicChanged.Count) {
    $appPath = Join-Path $wt 'public/app.js'; $htmlPath = Join-Path $wt 'public/index.html'
    $app = Read-Text $appPath; $html = Read-Text $htmlPath
    $headApp = (& git -C $wt show 'HEAD:public/app.js') -join "`n"
    $verRe = "const VIEW_MODULE_VERSION = '(\d+)';"
    $cur = [int]([regex]::Match($app, $verRe).Groups[1].Value)
    $old = [int]([regex]::Match($headApp, $verRe).Groups[1].Value)
    if ($cur -eq $old) {
        $new = $cur + 1
        Write-Text $appPath ([regex]::Replace($app, $verRe, "const VIEW_MODULE_VERSION = '$new';"))
        Write-Text $htmlPath ([regex]::Replace($html, 'app\.js\?v=\d+', "app.js?v=$new"))
        Note "view modules ?v=$cur -> $new"
    } else { Note "view modules already at ?v=$cur (HEAD $old)" }
    if ($ship | Where-Object { $_.Path -eq 'public/styles.css' }) {
        $html = Read-Text $htmlPath
        $headHtml = (& git -C $wt show 'HEAD:public/index.html') -join "`n"
        $cssRe = 'styles\.css\?v=(\d+)'
        $c1 = [int]([regex]::Match($html, $cssRe).Groups[1].Value); $c0 = [int]([regex]::Match($headHtml, $cssRe).Groups[1].Value)
        if ($c1 -eq $c0) { Write-Text $htmlPath ([regex]::Replace($html, $cssRe, "styles.css?v=$($c1 + 1)")); Note "styles.css ?v=$c1 -> $($c1 + 1)" }
    }
    # Stage 137: qz-print.js is a shell script of its own (qz-print.js?v=N);
    # it changed in 06547bb with no new N, so browsers could keep the old one.
    # 234.6: theme-boot.js and components/erp-typeahead.js are shell scripts too (?v=N).
    # Each is its path under public/, which is also how index.html names it.
    foreach ($shellJs in @('qz-print.js', 'theme-boot.js', 'components/erp-typeahead.js')) {
        if (-not ($ship | Where-Object { $_.Path -eq "public/$shellJs" })) { continue }
        $html = Read-Text $htmlPath
        $headHtml = (& git -C $wt show 'HEAD:public/index.html') -join "`n"
        $qzRe = [regex]::Escape($shellJs) + '\?v=(\d+)'
        $q1 = [int]([regex]::Match($html, $qzRe).Groups[1].Value); $q0 = [int]([regex]::Match($headHtml, $qzRe).Groups[1].Value)
        if ($q1 -eq $q0) { Write-Text $htmlPath ([regex]::Replace($html, $qzRe, "$shellJs`?v=$($q1 + 1)")); Note "$shellJs ?v=$q1 -> $($q1 + 1)" }
    }
} else { Note 'no public/ change' }

# ---------------------------------------------------- G4d PrintBridge download
# Stage 137.4: the ERP serves public/downloads/printbridge-setup.exe and offers
# an update from printbridge-version.txt. Both must be the agent's current
# release (../PrintBridge), or every computer is offered an old one.
Write-Gate 'G4d PrintBridge download'
$pbExe = Join-Path $wt 'public/downloads/printbridge-setup.exe'; $pbVerFile = Join-Path $wt 'public/downloads/printbridge-version.txt'
if (-not (Test-Path $pbExe) -or -not (Test-Path $pbVerFile)) { Stop-Release 'public/downloads/printbridge-setup.exe or printbridge-version.txt is missing - build it with ..\PrintBridge\installer\build.ps1 -Universal' }
$pbServed = (Read-Text $pbVerFile).Trim()
$pbSrc = Join-Path (Split-Path $repo -Parent) 'PrintBridge/agent/internal/agent/handlers.go'
if (Test-Path $pbSrc) {
    $pbAgent = [regex]::Match((Read-Text $pbSrc), 'BuildVersion\s*=\s*"([^"]+)"').Groups[1].Value
    if ($pbAgent -and $pbAgent -ne $pbServed) { Stop-Release "PrintBridge is $pbAgent but the ERP serves $pbServed - run ..\PrintBridge\installer\build.ps1 -Universal -OutDir <a temp folder> and copy its three files (printbridge-setup.exe, printbridge-update.json, printbridge-version.txt) into public\downloads" }
    Note "PrintBridge download $pbServed (agent source $pbAgent)"
} else { Note "PrintBridge download $pbServed (no ..\PrintBridge on this machine to compare)" }
# 203.3: from 0.3.0 installed agents replace themselves from
# printbridge-update.json. It must describe the served installer exactly
# (version, name, size, SHA-256), or every agent refuses the update - or never
# sees it. The signature itself is checked by the agents (and by
# `go run ./cmd/pbsign verify` in ..\PrintBridge\agent on the key's machine).
$pbUpd = Join-Path $wt 'public/downloads/printbridge-update.json'
$pbNeedsUpd = $false; try { $pbNeedsUpd = [version]$pbServed -ge [version]'0.3.0' } catch { }
if ($pbNeedsUpd -or (Test-Path $pbUpd)) {
    if (-not (Test-Path $pbUpd)) { Stop-Release "public/downloads/printbridge-update.json is missing - installed PrintBridge agents cannot update to $pbServed. build.ps1 -Universal writes it; copy it beside printbridge-setup.exe" }
    try { $pbM = (Read-Text $pbUpd) | ConvertFrom-Json } catch { Stop-Release "public/downloads/printbridge-update.json is not valid JSON: $($_.Exception.Message)" }
    $pbHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $pbExe).Hash.ToLowerInvariant()
    $pbSize = (Get-Item -LiteralPath $pbExe).Length
    $pbBad = @()
    if ($pbM.version -ne $pbServed) { $pbBad += "version $($pbM.version) (served $pbServed)" }
    if ($pbM.file -ne 'printbridge-setup.exe') { $pbBad += "file $($pbM.file)" }
    if ([int64]$pbM.size -ne $pbSize) { $pbBad += "size $($pbM.size) (exe $pbSize)" }
    if ("$($pbM.sha256)".ToLowerInvariant() -ne $pbHash) { $pbBad += 'sha256 differs from the exe' }
    if (-not $pbM.signature) { $pbBad += 'no signature' }
    if ($pbBad) { Stop-Release ("printbridge-update.json does not match the served installer: " + ($pbBad -join '; ') + " - copy all three files from one build.ps1 -Universal run") }
    Note "PrintBridge update file matches the installer ($pbServed)"
}

# --------------------------------------------------------- G4b Release number
# One number everywhere: the release decides it, internal/server/VERSION holds
# it (an unstamped build and the manuals' "release:" read that file), and
# Deploy.cmd ships exactly it. Until 2026-10-19 the number was worked out only
# at deploy time, so production said 0.4.0 while VERSION and the manuals still
# said 0.1.0. Same rule as cmd/releasever: a feat change moves MINOR, a
# docs-only change PATCH; MAJOR only by hand (deploy.ps1 -Version).
Write-Gate 'G4b Release number'
$live = $null
foreach ($h in $cfg.hosts) { try { $live = Invoke-RestMethod -Uri "https://$h/api/v1/version" -TimeoutSec 20; break } catch { } }
if (-not $live -or -not $live.version -or -not $live.git_commit -or $live.git_commit -eq 'dev') { Stop-Release 'cannot read the live release number from production (/api/v1/version)' }
& git -C $wt cat-file -e "$($live.git_commit)^{commit}" 2>$null
if ($LASTEXITCODE -ne 0) { Stop-Release "the live commit $($live.git_commit) is not in this repo's history" }
function Test-Code([string]$p) { $p -and $p -notlike 'docs/*' -and $p -notlike '*.md' }
# Code in the tree, or in commits made since origin outside this script: either
# means the full suite (G10) and a MINOR bump.
$codeShips = @($ship | Where-Object { Test-Code $_.Path }).Count -gt 0 -or
             @((Invoke-Git @('diff', '--name-only', 'origin/main', 'HEAD')) | Where-Object { Test-Code $_ }).Count -gt 0
$sinceLive = Invoke-Native -Exe $go -Arguments @('run', './cmd/releasever', '-previous', $live.version, '-since', $live.git_commit) -WorkDir $wt
$existing = ($sinceLive | Where-Object { $_ -match '^\d+\.\d+\.\d+$' } | Select-Object -Last 1)
$lv = [int[]]($live.version -split '\.'); $ev = [int[]]($existing -split '\.')
$releaseVersion = if ($ev[0] -gt $lv[0] -or $ev[1] -gt $lv[1]) { $existing }            # a feat commit since live already moved MINOR
                  elseif ($codeShips) { "$($lv[0]).$($lv[1] + 1).0" }                  # this release ships code: MINOR
                  elseif ($existing -ne $live.version) { $existing }                   # docs-only, PATCH already moved
                  else { "$($lv[0]).$($lv[1]).$($lv[2] + 1)" }                          # docs-only: PATCH
Write-Text (Join-Path $wt 'internal/server/VERSION') "$releaseVersion`n"
$state.version = $releaseVersion
$releaseKind = if ($codeShips) { 'feat' } else { 'docs' }
Note "release $releaseVersion (live $($live.version) at $($live.git_commit); this release ships $(if ($codeShips) { 'code' } else { 'docs only' }))"

# ------------------------------------------------- G4c budget, before the slow gates
# Its files are final once G4 has bumped the versions and nothing after G4 writes
# them, so it is judged here, not after the ~5 min of G5-G7 (0.11.0: stopped at
# G8 at 5:52 for 983 bytes).
Write-Gate 'G4c Release budget'
# 177.4: the stripped Linux server binary is measured here as well, built the
# way CI builds it (.github/workflows/ci.yml, "Enforce release artifact
# budgets"). Before, G4c never passed -binary, so every release passed while
# CI was red on every push at the binary budget.
# 177.5: that build takes 30-60 s, so it runs in the background at below-normal
# priority (Start-BudgetBuild) from the moment its inputs are final - right after
# G7's KB step, the last step that rewrites a file the server embeds - and is
# judged before G7's document register, with G4f (Wait-BudgetBuild). Here the
# cold core is judged at once, so an overrun still stops the click early.
$budgetBin = Join-Path (Join-Path $env:TEMP 'erp_release_budget') 'erp-server'
function Test-BudgetReport([string]$text, [switch]$Binary) {
    if ($text -match '"status":\s*"over') {
        $over = [regex]::Matches($text, '"(\w+)":\s*\{[^{}]*"value":\s*(\d+)[^{}]*"limit":\s*(\d+)[^{}]*"status":\s*"over_budget"') | ForEach-Object { "$($_.Groups[1].Value) $($_.Groups[2].Value) > $($_.Groups[3].Value)" }
        Stop-Release "release budget over: $($over -join '; '). The cold core counts view-pos.js too - move new code into a lazily loaded module, or condense comments in app.js / view-pos.js. binary_stripped_bytes is the stripped Linux server (NFR-COST-001): look for a new dependency or embedded file"
    }
    if ($text -notmatch '"cold_core_gzip_bytes"' -or ($Binary -and $text -notmatch '"binary_stripped_bytes"[^{}]*\{[^{}]*"status":\s*"within_budget"')) {
        Stop-Release "releasebudget gave no full report:`n$(($text -split "`n" | Select-Object -Last 15) -join "`n")"
    }
}
function Start-BudgetBuild {
    New-Item -ItemType Directory -Force -Path (Split-Path $budgetBin) | Out-Null
    Remove-Item -LiteralPath $budgetBin -ErrorAction SilentlyContinue
    $log = Join-Path $env:TEMP "release_budget_build_$ts.log"
    $saved = @{ CGO_ENABLED = $env:CGO_ENABLED; GOOS = $env:GOOS; GOARCH = $env:GOARCH }
    try {
        # The child takes this environment when it starts; the click's own stays Windows.
        $env:CGO_ENABLED = '0'; $env:GOOS = 'linux'; $env:GOARCH = 'amd64'
        $p = Start-Process -FilePath $go -ArgumentList "build -trimpath `"-ldflags=-s -w`" -o `"$budgetBin`" ./cmd/server" -WorkingDirectory $wt -RedirectStandardOutput $log -RedirectStandardError "$log.err" -NoNewWindow -PassThru
    } finally {
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    }
    $null = $p.Handle
    try { $p.PriorityClass = 'BelowNormal' } catch { }   # compile.exe inherits it
    $script:budgetBuild = [pscustomobject]@{ Proc = $p; Log = $log }
}
function Wait-BudgetBuild {
    $b = $script:budgetBuild
    $b.Proc.WaitForExit()
    $out = @(Get-Content -LiteralPath $b.Log -ErrorAction SilentlyContinue) + @(Get-Content -LiteralPath "$($b.Log).err" -ErrorAction SilentlyContinue)
    Remove-Item -LiteralPath $b.Log, "$($b.Log).err" -ErrorAction SilentlyContinue
    $script:budgetBuild = $null
    if ($b.Proc.ExitCode -ne 0) { Stop-Release "stripped Linux build (G4c) failed:`n$(($out | Select-Object -Last 20) -join "`n")" }
    $text = (Invoke-Native -Exe $go -Arguments @('run', './cmd/releasebudget', '-binary', $budgetBin) -WorkDir $wt -AllowFail) -join "`n"
    Remove-Item -LiteralPath $budgetBin -ErrorAction SilentlyContinue
    Test-BudgetReport $text -Binary
    $text
}
$budgetText = (Invoke-Native -Exe $go -Arguments @('run', './cmd/releasebudget') -WorkDir $wt -AllowFail) -join "`n"
Test-BudgetReport $budgetText
Note 'release budget: cold core within limits; the stripped Linux binary builds in the background from G7 and is judged before its register'

# ------------------------------------------------- G4e compliance reviews
# Stage 47.16.1a: every row of docs/security/compliance-control-matrix.json
# carries the date it is next reviewed by. `go test` only logs OVERDUE, so a
# calendar date never breaks a developer's build; a release does not ship
# while one has passed. Before the slow gates: it takes seconds.
Write-Gate 'G4e Compliance reviews'
$overdue = @(Get-OverdueComplianceReviews -RepoDir $wt -GoExe $go)
if ($overdue.Count) { Stop-Release "compliance review overdue in docs/security/compliance-control-matrix.json:`n$($overdue -join "`n")`nReview each row with its owner, then move its last_reviewed and review_by" }
Note 'compliance matrix: no review overdue'

# ------------------------------------------------- G5 generated Go and reports
Write-Gate 'G5 Report catalog spec'
# With -LocalTests it is always regenerated: a stale file committed outside
# Release.cmd must still be caught (accuracy before speed). With the tests on
# GitHub (177.5) it is regenerated only when its inputs ship, which saves a
# full engines compile here; the GitHub run's "Report catalog spec is current"
# step (gencatalog -check) catches a stale one before main moves.
$releasePaths = @($ship | ForEach-Object { $_.Path }) + @(Invoke-Git @('diff', '--name-only', 'origin/main', 'HEAD'))
$specInputs = @($releasePaths | Where-Object { $_ -like 'docs/specs/reports/*' -or $_ -like 'cmd/gencatalog/*' -or $_ -eq 'engines/report_catalog_spec_generated.go' })
if ($onGitHub -and -not $specInputs.Count) { Note 'catalog spec inputs unchanged (the GitHub run checks it)' }
elseif (Test-Path (Join-Path $wt 'cmd/gencatalog')) {
    $specTmp = Join-Path $env:TEMP "release_spec_$ts.go"
    Invoke-Native -Exe $go -Arguments @('run', './cmd/gencatalog', '-out', $specTmp) -WorkDir $wt | Out-Null
    $spec = Join-Path $wt 'engines/report_catalog_spec_generated.go'
    if ((Read-Text $specTmp).Replace("`r`n", "`n") -ne (Read-Text $spec).Replace("`r`n", "`n")) { Copy-Item $specTmp $spec -Force; Note 'catalog spec regenerated' } else { Note 'catalog spec current' }
    Remove-Item $specTmp -ErrorAction SilentlyContinue
} else { Note 'no catalog generator' }

# ------------------------------------------------------------ G6 attack surface
Write-Gate 'G6 Attack surface'
$surfTmp = Join-Path $env:TEMP "release_surface_$ts.json"
Invoke-Native -Exe $go -Arguments @('run', './cmd/surfacescan', '-out', $surfTmp) -WorkDir $wt | Out-Null
# G6b (Stage 49.17.1): what this release changes in the attack surface - the
# last release's manifest (HEAD) against the one just generated - into the
# report, high-risk lines (a route anyone can reach, changed authentication,
# a new dependency or outbound call) first. Read, not a stop.
$surfBase = Join-Path $env:TEMP "release_surface_base_$ts.json"
$baseLines = Invoke-Native -Exe 'git' -Arguments @('show', 'HEAD:docs/security/attack_surface.json') -WorkDir $wt -AllowFail
if ($baseLines.Count -gt 0 -and $baseLines[0] -eq '{') {
    ($baseLines -join "`n") | Set-Content -LiteralPath $surfBase -Encoding utf8NoBOM
    $impact = Invoke-Native -Exe $go -Arguments @('run', './cmd/securityimpact', '-base', $surfBase, '-new', $surfTmp) -WorkDir $wt -AllowFail
    $report.Add(''); $report.Add('Security impact (G6b):'); $impact | ForEach-Object { $report.Add("    $_") }
    $impact | Set-Content -LiteralPath (Join-Path $env:TEMP "release_impact_$ts.txt") -Encoding utf8NoBOM   # read again by G10b
    $highLines = @($impact | Where-Object { $_ -like '  ! *' } | ForEach-Object { $_.Substring(4).Trim() })
    $high = $highLines.Count
    Note "security impact: $high high-risk change(s), see the report"
    # 49.17.1 (owner, 2026-11-14: "all recommended"): a high-risk change stops
    # the release until the owner writes a one-line reason for it in
    # release\security-ack.txt (git-ignored, like hold.txt), one line each:
    #   <the change exactly as the report prints it after "! "> | <reason>
    # The reasons go into the report, so the release record says who accepted what.
    if ($high) {
        $ackFile = Join-Path $ReleaseDir 'security-ack.txt'
        $acks = @{}
        if (Test-Path $ackFile) {
            foreach ($l in Get-Content -LiteralPath $ackFile) {
                if ($l -match '^\s*(#|$)') { continue }
                $i = $l.IndexOf('|')   # the change text never holds one; a reason may
                if ($i -gt 0 -and $l.Substring($i + 1).Trim()) { $acks[$l.Substring(0, $i).Trim()] = $l.Substring($i + 1).Trim() }
            }
        }
        $unread = @($highLines | Where-Object { -not $acks.ContainsKey($_) })
        if ($unread.Count) {
            Stop-Release ("security impact: {0} high-risk change(s) have no reason in release\security-ack.txt. Add one line each, '<change> | <reason>':`n{1}" -f $unread.Count, (($unread | ForEach-Object { "    $_ | " }) -join "`n"))
        }
        $report.Add('    Accepted by the owner (release\security-ack.txt):')
        foreach ($h in $highLines) { $report.Add("      $h - $($acks[$h])") }
        Note "security impact: every high-risk change has the owner's reason"
    }
    Remove-Item $surfBase -ErrorAction SilentlyContinue
} else { Note 'security impact: no manifest at HEAD to compare' }
Copy-Item $surfTmp (Join-Path $wt 'docs/security/attack_surface.json') -Force
Remove-Item $surfTmp -ErrorAction SilentlyContinue
Note 'attack_surface.json regenerated from the release set'   # the full suite's surface test re-checks it

# ---------------------------------------------------------------- G7 docs
Write-Gate 'G7 Guides, KB, brain, register'
# 177.5: the code map's file list is exact here (every file is filed in a region
# below). Its counts come from the code graph of the last release: rebuilding the
# graph takes minutes, so Invoke-BrainRedraw.ps1 does it after the push and Deploy
# (D6) brings the exact map in. The graph comes from the last redraw, else the
# shared tree's copy; with neither, it is built here once.
$graphDir = Join-Path $wt 'graphify-out'
$sharedGraph = Join-Path $repo 'graphify-out/graph.json'
if (Test-Path -LiteralPath $graphKeep) { Move-Item -LiteralPath $graphKeep -Destination $graphDir }
elseif (Test-Path -LiteralPath $sharedGraph) {
    New-Item -ItemType Directory -Force -Path $graphDir | Out-Null
    Copy-Item -LiteralPath $sharedGraph -Destination $graphDir
}
if (-not (Test-Path -LiteralPath (Join-Path $graphDir 'graph.json'))) {
    Note 'no code graph yet - building it once (a few minutes)'
    $run = Get-GraphifyRun
    $graphOut = Invoke-Native -Exe $run.Exe -Arguments $run.Arguments -WorkDir $wt -AllowFail
    if ($LASTEXITCODE -ne 0) { Stop-Release "graphify failed:`n$(($graphOut | Select-Object -Last 10) -join "`n")" }
}
$kb = Invoke-Docs 'KB'
if (-not $kb.ok) { Stop-Release "update-docs -Group KB failed:`n$(($kb.out | Select-Object -Last 15) -join "`n")" }
# 177.5: the KB content is embedded in the server and was the last such file
# to change, so G4c's stripped Linux build starts now and runs beside the rest of G7.
Start-BudgetBuild
# G7's own steps are timed apart (KB above; map and brain; register) to see what to cut next.
Write-Gate 'G7 Checklist map, brain'
# The checklist's module map (2026-11-10): counts and empty-sub-module markers are refreshed
# here, so a session that forgot to run it never ships stale numbers; a skeleton that differs
# between the tracker and its archive, an open item in the archive or an item outside a module
# stops the release.
$map = Invoke-Native -Exe 'pwsh' -Arguments @('-NoProfile', '-File', 'docs/update-checklist-map.ps1') -WorkDir $wt -AllowFail
if ($LASTEXITCODE -ne 0) { Stop-Release "checklist module map (docs/update-checklist-map.ps1):`n$(($map | Select-Object -Last 10) -join "`n")" }
Note (($kb.out | Where-Object { $_ -match 'Published' }) -join ' ')
# brainmap itself names the files no region claims (update-docs only reports
# that the generator failed), so ask it first.
$brainTmp = Join-Path $env:TEMP "release_brain_$ts"
$probe = Invoke-Native -Exe $go -Arguments @('run', './cmd/brainmap', '-out', $brainTmp) -WorkDir $wt -AllowFail
Remove-Item $brainTmp -Recurse -Force -ErrorAction SilentlyContinue
$unclaimed = @($probe | Where-Object { $_ -match '^\s{2}\S' } | ForEach-Object { $_.Trim() } | Where-Object { Test-Path -LiteralPath (Join-Path $wt $_) })
if ($unclaimed.Count) {
    # A new file no region claims: file it beside its nearest sibling in the
    # same folder (2026-10-19: stock_location_txn_defaults.go went next to
    # stock_locations.go). The settings.ignore list is never a sibling.
    $mapPath = Join-Path $wt 'docs/brain/brain.map.json'
    $lines = [System.Collections.Generic.List[string]]((Read-Text $mapPath) -split "`n")
    foreach ($f in $unclaimed) {
        if (-not $f.Contains('/')) { Stop-Release "$f is at the repo root and no region claims it - add it to brain.map.json (a region, or settings.ignore like Open-ERP.cmd)" }
        $dir = ($f -replace '/[^/]+$', '') + '/'
        $inIgnore = $false; $best = -1; $bestLen = -1
        for ($i = 0; $i -lt $lines.Count; $i++) {
            if ($lines[$i] -match '"ignore"\s*:\s*\[') { $inIgnore = $true; continue }
            if ($inIgnore) { if ($lines[$i] -match '^\s*\]') { $inIgnore = $false }; continue }
            if ($lines[$i] -match '^\s*"([^"*]+)",?\s*$' -and $Matches[1].StartsWith($dir)) {
                $sib = $Matches[1]
                if ($sib.Substring($dir.Length).Contains('/')) { continue }   # same folder only
                $len = 0; while ($len -lt [Math]::Min($sib.Length, $f.Length) -and $sib[$len] -eq $f[$len]) { $len++ }
                if ($len -gt $bestLen) { $best = $i; $bestLen = $len }
            }
        }
        if ($best -lt 0) { Stop-Release "no brain region holds a file in $dir - add $f to docs/brain/brain.map.json by hand" }
        $indent = [regex]::Match($lines[$best], '^\s*').Value
        if (-not $lines[$best].TrimEnd().EndsWith(',')) { $lines[$best] = $lines[$best].TrimEnd() + ',' }
        $comma = if ($lines[$best + 1] -match '^\s*\]') { '' } else { ',' }
        $lines.Insert($best + 1, "$indent`"$f`"$comma")
        Note "brain: $f filed beside $(($lines[$best].Trim()).Trim(','))"
    }
    Write-Text $mapPath ($lines -join "`n")
}
$brain = Invoke-Docs 'Brain'
if (-not $brain.ok) { Stop-Release "brain redraw failed:`n$(($brain.out | Select-Object -Last 15) -join "`n")" }
Note 'brain: every file filed; counts from the last graph, exact map after the push'

# G4c's second half and G4f come before the register: the register records the
# footprint ledger's bytes and hash, so the ledger must be final first.
Write-Gate 'G4c Release budget: stripped binary'
$budgetText = Wait-BudgetBuild
Note 'release budget within limits (stripped Linux binary included)'

# ------------------------------------------------- G4f footprint ledger
# 162.21 (user, 2026-11-11): each release records one entry in the footprint
# ledger (cmd/epicledger, docs/qa/epic-ledger.json), from the budget report just
# taken, so the next epic has a point to compare with. Warn-only: a missing entry
# never stops a release. Its commit is the one this release is built on.
Write-Gate 'G4f Footprint ledger'
$budgetFile = Join-Path $env:TEMP "release_budget_$ts.json"
$i, $j = $budgetText.IndexOf('{'), $budgetText.LastIndexOf('}')
if ($i -ge 0 -and $j -gt $i) {
    Write-Text $budgetFile $budgetText.Substring($i, $j - $i + 1)
    $ledgerOut = Invoke-Native -Exe $go -Arguments @('run', './cmd/epicledger', 'record', '-id', "release-$releaseVersion", '-label', "release $releaseVersion", '-root', '.', '-budget', $budgetFile, '-replace') -WorkDir $wt -AllowFail
    if ($LASTEXITCODE -ne 0) { Note "WARNING footprint ledger not recorded: $(($ledgerOut | Select-Object -Last 3) -join ' ')" }
    else { Note "footprint ledger: release-$releaseVersion recorded" }
    Remove-Item -LiteralPath $budgetFile -ErrorAction SilentlyContinue
} else { Note 'WARNING footprint ledger not recorded: no budget report' }

Write-Gate 'G7 Document register'
$regTmp = Join-Path $env:TEMP "release_register_$ts.json"
$lint = Invoke-Native -Exe $go -Arguments @('run', './cmd/doclint', '-root', '.', '-write-register', $regTmp) -WorkDir $wt -AllowFail
if (-not (Test-Path $regTmp)) { Stop-Release 'doclint could not write the document register' }
Copy-Item $regTmp (Join-Path $wt 'docs/governance/document-register.json') -Force
Remove-Item $regTmp -ErrorAction SilentlyContinue
# One doclint run: the register it just wrote is the inventory, so nothing is
# unregistered or missing any more; its broken-link findings stand as they are.
$lintBad = @($lint | Where-Object { $_ -match 'broken' })
if ($lintBad.Count) { Stop-Release "doclint: $($lintBad.Count) broken link / register problem(s):`n$(($lintBad | Select-Object -First 10) -join "`n")" }
Note 'document register rewritten; no broken links'

# ---------------------------------------------------------------- G8 static
Write-Gate 'G8 Build, vet, JS'
# Build, vet and compiling the two test programs G10 runs only read
# the tree, so they run side by side (Stage 109.10); the Go build cache is safe
# to share between them.
function Start-GoJob([string]$name, [string[]]$a) {
    # The name goes into a file name: "compile internal/server tests" once made a
    # folder path that did not exist (2026-10-19).
    $log = Join-Path $env:TEMP ("release_g8_{0}_{1}.log" -f ($name -replace '[^A-Za-z0-9_-]', '_'), $ts)
    $p = Start-Process -FilePath $go -ArgumentList $a -WorkingDirectory $wt -RedirectStandardOutput $log -RedirectStandardError "$log.err" -NoNewWindow -PassThru
    $null = $p.Handle
    [pscustomobject]@{ Name = $name; Proc = $p; Log = $log }
}
$testExes = [System.Collections.Generic.List[string]]::new()
# 177.1: on GitHub the Release workflow builds, vets and compiles the same commit
# before anything else, so the laptop only checks the JS here.
$goJobs = @()
if (-not $onGitHub) {
    $goJobs = @((Start-GoJob 'build' @('build', './...')), (Start-GoJob 'vet' @('vet', './...')))
    foreach ($p in @('engines', 'internal/server')) {
        $exe = Join-Path $env:TEMP "release_$($p -replace '/', '_')_$ts.test.exe"
        $testExes.Add($exe)
        $goJobs += Start-GoJob "compile $p tests" @('test', '-c', '-o', $exe, "./$p")
    }
}
$jsFiles = @($ship | Where-Object { $_.Code -ne ' D' -and $_.Path -like '*.js' } | ForEach-Object Path) + @('public/app.js') | Select-Object -Unique
foreach ($js in $jsFiles) { Invoke-Native -Exe 'node' -Arguments @('--check', (Join-Path $wt $js)) | Out-Null }
Note "node --check: $($jsFiles.Count) file(s)"
# 221.1: a page drawn as an SVG image takes its CSS through svgStyle() in print-pdf.js,
# which escapes it for XML. 0.25.0 shipped a "<style>" inside a CSS comment, the raw
# copy made the image undecodable, and every silent bill print failed. The browser
# smoke run (G10a) is optional, so this check is static and always runs.
$rawSvgCss = @(Get-ChildItem -LiteralPath (Join-Path $wt 'public') -Filter '*.js' -Recurse -File | Where-Object {
        $t = Get-Content -LiteralPath $_.FullName -Raw
        $t -match 'image/svg\+xml' -and $t -match '<style>(\$\{|[''"`]\s*\+)'
    } | ForEach-Object { $_.FullName.Substring($wt.Length + 1) })
$rawSvgCss = @($rawSvgCss | Where-Object { $_ -ne 'public\print-pdf.js' -or (Get-Content -LiteralPath (Join-Path $wt $_) -Raw) -notmatch '(?s)export function svgStyle\(css\) \{\s*return `<style>\$\{String\(css\)\.replace\(/&/g, ''&amp;''\)\.replace\(/</g, ''&lt;''\)\}</style>`' })
if ($rawSvgCss.Count) { Stop-Release "CSS put raw into an SVG image (use svgStyle from print-pdf.js): $($rawSvgCss -join ', ')" }
Note 'SVG image CSS: escaped through svgStyle'
foreach ($j in $goJobs) {
    $j.Proc.WaitForExit()
    $out = @(Get-Content -LiteralPath $j.Log -ErrorAction SilentlyContinue) + @(Get-Content -LiteralPath "$($j.Log).err" -ErrorAction SilentlyContinue)
    Remove-Item -LiteralPath $j.Log, "$($j.Log).err" -ErrorAction SilentlyContinue
    if ($j.Proc.ExitCode -ne 0) { Stop-Release "go $($j.Name) failed:`n$(($out | Select-Object -Last 20) -join "`n")" }
}
Note $(if ($onGitHub) { 'go build + go vet: in the GitHub run' } else { 'go build + go vet clean; test programs compiled' })

# ---------------------------------------------------- G9 data-change scan
Write-Gate 'G9 What this changes in client data'
Invoke-Git @('add', '-A') $wt | Out-Null
$newMigrations = @((Invoke-Git @('diff', '--cached', '--name-only', '--diff-filter=A', 'HEAD', '--', 'db/') $wt) | Where-Object { $_ -like '*.sql' } | ForEach-Object { Join-Path $wt $_ })
$goDiff = Invoke-Git @('diff', '--cached', 'HEAD', '--', '*.go') $wt
$findings = Get-DataChangeFindings -MigrationFiles $newMigrations -AddedGoLines @(Get-AddedGoLines $goDiff)
$state.findings = @($findings | ForEach-Object { "[$($_.Kind)] $($_.File):$($_.Line) $($_.Text)" })
Format-Findings $findings | ForEach-Object { Note $_ }
Note "$($newMigrations.Count) new migration(s); the Deploy click asks before running any [client data] / [DESTRUCTIVE] change"
# G9b (2026-10-26): a deploy/ script this release changes reaches the server only if
# deploy/deploy.ps1 ships it - 0.15.0 changed restore_drill.sh and the server kept the August copy.
$deployChanged = @((Invoke-Git @('diff', '--cached', '--name-only', 'HEAD', '--', 'deploy/') $wt) | Where-Object { $_ -match '^deploy/[^/]+\.(sh|service)$' })
if ($deployChanged.Count) {
    $shipper = Get-Content -LiteralPath (Join-Path $wt 'deploy/deploy.ps1') -Raw
    foreach ($f in $deployChanged) {
        $name = Split-Path $f -Leaf
        if ($shipper -notmatch [regex]::Escape($name)) { Note "deploy/$name changed but deploy/deploy.ps1 does not ship it - the server keeps its old copy (add it to deploy.ps1's list, or copy it by hand after the deploy)" }
        else { Note "deploy/$name changed - the deploy ships it" }
    }
}
# G9c (2026-10-31): a new retention / cleanup setting ships Off. 0.20.0 added
# platform.import_file_retention_days at 30, so the hourly sweep deleted import files and
# failed rows on app and minn the day it deployed. A setting this release adds whose key
# names a retention, cleanup, purge or expiry must default to "0" or "" - the client picks
# the value. A reviewed exception says so on its Key line: // G9c-ok: <why>.
$settingsAdded = @((Invoke-Git @('diff', '--cached', '-U0', 'HEAD', '--', 'engines/settings_definitions.go') $wt) | Where-Object { $_ -like '+*' -and $_ -notlike '+++*' } | ForEach-Object { $_.Substring(1) })
$pendingKey = $null; $keyOk = $false
foreach ($l in $settingsAdded) {
    if ($l -match 'Key:\s*"([^"]+)"') { $pendingKey = $Matches[1]; $keyOk = $l -match 'G9c-ok' }
    if ($pendingKey -and $l -match 'Default:\s*"([^"]*)"') {
        $def = $Matches[1]
        if (-not $keyOk -and $pendingKey -match '(?i)retention|cleanup|purge|expir|sweep|delete_after|keep_days' -and $def -notin @('', '0')) {
            Stop-Release "new setting $pendingKey defaults to `"$def`": a retention / cleanup setting ships Off (Default `"0`") so a deploy never starts deleting client data; or mark its Key line // G9c-ok: <why> (G9c)"
        }
        $pendingKey = $null
    }
}

# ------------------------------------------------- G10 fresh database + tests
# Every migration from empty (the shared custom_erp hides ordering and
# from-zero bugs), then the suite. Packages share test data, so each test
# process gets its own copy of the migrated database (CREATE DATABASE ...
# TEMPLATE, a file copy) and up to four run side by side: the engines tests in
# three groups, internal/server, then the small packages. One package at a
# time on one database took ~15 minutes (Stage 109.9). Four at a time, not
# more: a test process may open 50 connections and local Postgres allows 100.
# Every release runs the whole suite (a docs change can break server tests).
Write-Gate 'G10 Fresh database, migrations, tests'
$testTimesPath = Join-Path $wt 'release/test-times.json'
function Save-TestTimes([hashtable]$t) {
    # Sorted by name and rounded, so a release's diff of this file stays small.
    $o = [ordered]@{}
    foreach ($k in ($t.Keys | Sort-Object -CaseSensitive)) { $o[$k] = [math]::Round([double]$t[$k], 2) }
    Write-Text $testTimesPath ((($o | ConvertTo-Json) -replace "`r`n", "`n") + "`n")
}
if ($onGitHub) {
    # 177.1: the GitHub run migrates an empty database and runs every test of this
    # exact commit. Here the split's times are only brought up to date from the
    # last GitHub run's shards, so its 16 shards stay even.
    $ranCount = 'on GitHub'
    $times = Read-TestTimes $testTimesPath
    Push-Location $repo   # gh finds the GitHub repository from the folder's origin
    try {
        $lastRun = @((& gh run list --workflow release.yml --status completed --limit 1 --json databaseId 2>$null) | ConvertFrom-Json)
        if ($lastRun.Count) {
            $dl = Join-Path $env:TEMP "release_times_$ts"
            & gh run download $lastRun[0].databaseId --pattern 'shard-*' --dir $dl 2>&1 | Out-Null
            $n = 0
            foreach ($f in @(Get-ChildItem -LiteralPath $dl -Recurse -Filter 'times-*.json' -ErrorAction SilentlyContinue)) {
                (Get-Content -LiteralPath $f.FullName -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { $times[$_.Name] = [double]$_.Value; $n++ }
            }
            Remove-Item $dl -Recurse -Force -ErrorAction SilentlyContinue
            if ($n) { Save-TestTimes $times }
            Note "test times: $n taken from GitHub run $($lastRun[0].databaseId)"
        }
    } catch { Note "WARNING test times: not refreshed from GitHub ($($_.Exception.Message))" }
    finally { Pop-Location }
    Note 'tests: on GitHub after the push - migrate from empty and the whole suite on this exact commit; main moves only when every job passes'
} else {
function Get-DbUrl([string]$name) { "postgres://$($cfg.pgUser)@$($cfg.pgHost):$($cfg.pgPort)/$($name)?sslmode=disable" }
Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + "CREATE DATABASE $scratchDb") | Out-Null
$serverExe = Join-Path $env:TEMP "release_erp_server_$ts.exe"
Invoke-Native -Exe $go -Arguments @('build', '-o', $serverExe, './cmd/server') -WorkDir $wt | Out-Null
$copies = [System.Collections.Generic.List[string]]::new()   # $testExes was compiled in G8
$logDir = Join-Path $env:TEMP "release_tests_$ts"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$savedPath = $env:PATH
# `go test` puts Go's own bin first on PATH for tests that run go; the compiled
# test binaries below are started directly, so do the same.
$env:PATH = "$goDir;$env:PATH"
try {
    $env:DATABASE_URL = Get-DbUrl $scratchDb; $env:TEST_DATABASE_URL = $env:DATABASE_URL
    $mig = Invoke-Native -Exe $serverExe -Arguments @('-migrate') -WorkDir $env:TEMP -AllowFail
    if ($LASTEXITCODE -ne 0) { Stop-Release "migrate from empty failed:`n$(($mig | Select-Object -Last 15) -join "`n")" }
    if ($mig | Where-Object { $_ -match '\[sync-warn\]' }) { Stop-Release "migrate printed [sync-warn]:`n$(($mig | Where-Object { $_ -match 'sync-warn' }) -join "`n")" }
    Note (($mig | Where-Object { $_ -match '^Applied' }) -join ' ')

    # 177.7: the guard tests (release\guard-tests.txt) first, on their own copy of
    # the database: a missed registration stops the release here, in about a
    # minute, not when the last job ends (ask 174 found nine of them last).
    $guardDb = "$($scratchDb)_guards"
    Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + "CREATE DATABASE $guardDb TEMPLATE $scratchDb") | Out-Null
    $copies.Add($guardDb)
    $env:DATABASE_URL = Get-DbUrl $guardDb; $env:TEST_DATABASE_URL = $env:DATABASE_URL
    $guardOut = @(& (Join-Path $PSScriptRoot 'Invoke-GuardTests.ps1') -Root $wt -Go $go -Exes @{ 'engines' = $testExes[0]; 'internal/server' = $testExes[1] })
    if ($LASTEXITCODE -ne 0) { Stop-Release (($guardOut | Select-Object -Last 1) -join '') }
    Note (($guardOut | Select-Object -Last 1) -join '')

    # The jobs.
    $pkgs = @((Invoke-Native -Exe $go -Arguments @('list', '-f', '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}', './...') -WorkDir $wt) | Where-Object { $_ })
    $jobs = [System.Collections.Generic.List[object]]::new()
    $times = @{}
    function New-TestJob($name, $kind, $pkg, $exe, $dir, $run, $pkgList) {
        [pscustomobject]@{ Name = $name; Kind = $kind; Pkg = $pkg; Exe = $exe; Dir = $dir; Run = $run; Pkgs = $pkgList; Db = ''; Out = ''; Proc = $null; Code = $null; Expect = @() }
    }
    # Always the whole suite, docs-only or not: server tests check the help
    # links into KB articles, so a docs change can break them.
    if ($true) {
        $engDir = Join-Path $wt 'engines'
        # Tests, examples and fuzz seeds - everything `go test` runs (benchmarks it skips).
        $names = @((Invoke-Native -Exe $testExes[0] -Arguments @('-test.list', '.*') -WorkDir $engDir) | Where-Object { $_ -match '^(Test|Example|Fuzz)\w*$' } | Sort-Object)
        # Three groups of equal running time (Split-TestsByTime in ReleaseCommon.ps1,
        # the same split the GitHub shards make), from each test's time in the last
        # run: release\test-times.json, committed with each release.
        $times = Read-TestTimes $testTimesPath
        $groups = 3
        $split = Split-TestsByTime -Names $names -Times $times -Groups $groups
        $bucket = $split.Buckets; $load = $split.Load
        for ($g = 0; $g -lt $groups; $g++) {
            $jobs.Add((New-TestJob "engines$($g + 1)" 'exe' 'custom_erp/engines' $testExes[0] $engDir "^($($bucket[$g] -join '|'))$" @()))
            $jobs[$jobs.Count - 1].Expect = @($bucket[$g])
        }
        Note ("engines groups by last run's times: {0}" -f (($load | ForEach-Object { '{0:n0}s' -f $_ }) -join ' / '))
        $srvDir = Join-Path $wt 'internal/server'
        $srvNames = @((Invoke-Native -Exe $testExes[1] -Arguments @('-test.list', '.*') -WorkDir $srvDir) | Where-Object { $_ -match '^(Test|Example|Fuzz)\w*$' })
        $jobs.Add((New-TestJob 'server' 'exe' 'custom_erp/internal/server' $testExes[1] $srvDir "^($($srvNames -join '|'))$" @()))
        $jobs[$jobs.Count - 1].Expect = $srvNames
        $jobs.Add((New-TestJob 'small' 'go' '' $go $wt '' @($pkgs | Where-Object { $_ -notin @('custom_erp/engines', 'custom_erp/internal/server') })))
    }

    # Run them all at once, each on its own database copy: four database-heavy
    # jobs (a test process may open 50 connections; local Postgres allows 100)
    # plus the small packages, which barely touch the database.
    $queue = [System.Collections.Generic.Queue[object]]::new($jobs)
    $running = [System.Collections.Generic.List[object]]::new()
    $clock = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "   $($jobs.Count) test job(s) side by side..."
    while ($queue.Count -or $running.Count) {
        while ($queue.Count -and $running.Count -lt 5) {
            $j = $queue.Dequeue()
            $j.Db = "$($scratchDb)_$($j.Name)"
            Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + "CREATE DATABASE $($j.Db) TEMPLATE $scratchDb") | Out-Null
            $copies.Add($j.Db)
            $env:DATABASE_URL = Get-DbUrl $j.Db; $env:TEST_DATABASE_URL = $env:DATABASE_URL
            $j.Out = Join-Path $logDir "$($j.Name).log"
            # -test.v on the engines groups records each test's time for the next split.
            $a = if ($j.Kind -eq 'exe') { @('-test.count=1', "-test.timeout=$($cfg.testTimeout)") + $(if ($j.Run) { @('-test.v', '-test.run', $j.Run) } else { @() }) }
                 else { @('test') + $j.Pkgs + @('-p', '1', '-count=1', '-timeout', $cfg.testTimeout) }
            $j.Proc = Start-Process -FilePath $j.Exe -ArgumentList $a -WorkingDirectory $j.Dir -RedirectStandardOutput $j.Out -RedirectStandardError "$($j.Out).err" -NoNewWindow -PassThru
            $null = $j.Proc.Handle   # keeps the exit code readable after the process ends
            $running.Add($j)
        }
        Start-Sleep -Milliseconds 500
        foreach ($j in @($running | Where-Object { $_.Proc.HasExited })) {
            $j.Proc.WaitForExit(); $j.Code = $j.Proc.ExitCode; $running.Remove($j) | Out-Null
            Write-Host ("   {0,-9} {1} at {2:mm\:ss}" -f $j.Name, $(if ($j.Code -eq 0) { 'ok' } else { "FAILED ($($j.Code))" }), $clock.Elapsed)
        }
    }
    # Each engines test's time, kept for the next run's split.
    $newTimes = @{} + $times
    foreach ($j in $jobs | Where-Object { $_.Run }) {
        foreach ($l in Get-Content -LiteralPath $j.Out) { if ($l -match '^--- (PASS|FAIL|SKIP): (Test\w+) \(([\d.]+)s\)') { $newTimes[$Matches[2]] = [double]$Matches[3] } }
    }
    if ($newTimes.Count) { Save-TestTimes $newTimes }
    $testLog = Join-Path $reportDir "release-$ts-tests.log"
    $all = foreach ($j in $jobs) { "===== $($j.Name)"; Get-Content -LiteralPath $j.Out; Get-Content -LiteralPath "$($j.Out).err" -ErrorAction SilentlyContinue }
    $all | Set-Content -LiteralPath $testLog -Encoding utf8

    # Which tests failed; a failure that passes on rerun (alone, on the same copy) is flaky.
    $failed = [System.Collections.Generic.List[object]]::new()
    foreach ($j in $jobs | Where-Object { $_.Code -ne 0 }) {
        $lines = @(Get-Content -LiteralPath $j.Out) + @(Get-Content -LiteralPath "$($j.Out).err" -ErrorAction SilentlyContinue)
        if ($lines | Where-Object { $_ -match '\[build failed\]|^panic:|setup failed' }) { Stop-Release "test job $($j.Name) did not build or panicked - see $testLog" }
        $pending = @(); $found = 0
        foreach ($l in $lines) {
            if ($l -match '^\s*--- FAIL: (\w+)') { $pending += $Matches[1] }
            elseif ($j.Kind -eq 'go' -and $l -match '^(FAIL|ok)\s+(custom_erp/\S+)') { foreach ($t in $pending | Select-Object -Unique) { $failed.Add([pscustomobject]@{ Job = $j; Pkg = $Matches[2]; Test = $t }); $found++ }; $pending = @() }
        }
        if ($j.Kind -eq 'exe') { foreach ($t in $pending | Select-Object -Unique) { $failed.Add([pscustomobject]@{ Job = $j; Pkg = $j.Pkg; Test = $t }); $found++ } }
        if (-not $found) { Stop-Release "test job $($j.Name) failed without a named test - see $testLog" }
    }
    $still = @()
    foreach ($f in $failed) {
        $env:DATABASE_URL = Get-DbUrl $f.Job.Db; $env:TEST_DATABASE_URL = $env:DATABASE_URL
        $passed = $false
        for ($r = 1; $r -le [int]$cfg.flakyReruns -and -not $passed; $r++) {
            if ($f.Job.Kind -eq 'exe') { Invoke-Native -Exe $f.Job.Exe -Arguments @('-test.run', "^$($f.Test)$", '-test.count=1') -WorkDir $f.Job.Dir -AllowFail | Out-Null }
            else { Invoke-Native -Exe $go -Arguments @('test', $f.Pkg, '-run', "^$($f.Test)$", '-count=1') -WorkDir $wt -AllowFail | Out-Null }
            $passed = ($LASTEXITCODE -eq 0)
        }
        if ($passed) { $state.flaky += "$($f.Pkg) $($f.Test)"; Note "flaky (failed once, passed on rerun): $($f.Test)" } else { $still += "$($f.Pkg) $($f.Test)" }
    }
    if ($still.Count) { Stop-Release "tests fail: $($still -join ', ') - see $testLog" }
    # More than a few flaky tests is a pattern, not luck - stop rather than ship it.
    if ($state.flaky.Count -gt 3) { Stop-Release "$($state.flaky.Count) tests failed and then passed on rerun ($($state.flaky -join ', ')) - too many to call flaky; see $testLog" }
    # Every test ran: each group must report a result for every test it was
    # given (splitting the suite must never drop one silently).
    foreach ($j in $jobs | Where-Object { $_.Expect.Count }) {
        $seen = @{}
        foreach ($l in Get-Content -LiteralPath $j.Out) { if ($l -match '^--- (PASS|FAIL|SKIP): (\w+) \(') { $seen[$Matches[2]] = $true } }
        $missing = @($j.Expect | Where-Object { -not $seen.ContainsKey($_) })
        if ($missing.Count) { Stop-Release "test job $($j.Name) never reported $($missing.Count) of its $($j.Expect.Count) tests ($(($missing | Select-Object -First 5) -join ', ')) - see $testLog" }
    }
    $ranCount = ($jobs | ForEach-Object { $_.Expect.Count } | Measure-Object -Sum).Sum
    Note "all $ranCount engines + server tests, examples and fuzz seeds reported a result"
    Note ("tests green: {0} in {1:mm\:ss} ({2})" -f (($jobs | ForEach-Object Name) -join ', '), $clock.Elapsed, 'engines in 3 groups, each job on its own database copy')
} finally {
    $env:PATH = $savedPath
    Remove-Item Env:\DATABASE_URL, Env:\TEST_DATABASE_URL -ErrorAction SilentlyContinue
    Remove-Item $serverExe -ErrorAction SilentlyContinue
    foreach ($e in $testExes) { Remove-Item $e -ErrorAction SilentlyContinue }
    Remove-Item $logDir -Recurse -Force -ErrorAction SilentlyContinue
}
foreach ($db in @($copies) + $scratchDb) { Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + "DROP DATABASE IF EXISTS $db") -AllowFail | Out-Null }
}   # end of the -LocalTests G10

# ------------------------------------------------------- G10a browser smoke run
# 158.4: the snapshot's own server and screens in a real browser, through Guide (the bug
# inspector, outside the repo - config.json "guide"): admin and cashier1 signed in, the POS
# screen, one sale end to end (PO -> GRN -> sale -> same-day return, numbers read back) and
# the Sales Register showing that bill. The tests prove the rules; this proves the app opens
# and sells. Its migrations also run over a copy of the dev data (custom_erp_smoke, cloned
# once and reused), where G10 only migrates an empty database. Stops on an uncaught browser
# error, a failed request or 5xx, a step that could not be done, a PANIC row, or a setup
# failure of the release's own (migrate, server start); a number that differs between two
# places and the server's other error rows are warnings. Skipped, with a note, when Guide,
# Node, Playwright or Edge is not on this machine.
Write-Gate 'G10a Browser smoke run'
$guidePs1 = if ($cfg.PSObject.Properties['guide']) { [Environment]::ExpandEnvironmentVariables([string]$cfg.guide) } else { '' }
$smokeResult = 'not run'
if ($onGitHub) {
    # 177.2: the smoke run needs this laptop's Guide, Edge and dev data and takes minutes;
    # the click no longer waits for it. Run it on its own with -LocalTests when wanted.
    $smokeResult = 'not run (tests on GitHub)'
    Note 'smoke: not run - the tests run on GitHub (-LocalTests runs it here)'
} elseif (-not $guidePs1 -or -not (Test-Path -LiteralPath $guidePs1)) {
    Note "WARNING smoke: SKIPPED - Guide is not at '$guidePs1' on this machine"
} else {
    # The snapshot's tree before and after: G11 commits whatever is in it, so nothing may be left behind.
    Invoke-Git @('add', '-A') $wt | Out-Null; $treeBefore = (Invoke-Git @('write-tree') $wt)[0]
    $smokePath = $env:PATH; $env:PATH = "$goDir;$env:PATH"   # Guide builds with the go on PATH: the release's own
    try {
        $smokeOut = Invoke-Native -Exe 'pwsh' -Arguments @('-NoProfile', '-File', $guidePs1, '-Suite', 'smoke', '-Roles', 'admin,cashier1',
            '-WorkingTree', '-Repo', $wt, '-Db', 'custom_erp_smoke', '-BinSuffix', 'release', '-PgPort', [string]$cfg.pgPort) -AllowFail
        $smokeCode = $LASTEXITCODE
    } finally { $env:PATH = $smokePath }
    Invoke-Git @('add', '-A') $wt | Out-Null; $treeAfter = (Invoke-Git @('write-tree') $wt)[0]
    if ($treeAfter -ne $treeBefore) {
        $stray = Invoke-Git @('diff', '--name-only', $treeBefore, $treeAfter) $wt   # an array already: Invoke-Native returns one
        Stop-Release "the smoke run wrote into the release snapshot, so it would ship: $(($stray | Select-Object -First 8) -join ', ')"
    }
    $setup = @($smokeOut | Where-Object { $_ -match 'SETUP FAILED: (.+)$' } | ForEach-Object { $_ -replace '^.*SETUP FAILED: ', '' })
    $runDir = ''
    foreach ($l in $smokeOut) { if ($l -match '^\[guide\] report: (.+)\\report\.html$') { $runDir = $Matches[1] } }
    if ($setup.Count -and $setup[0] -match 'node is not on PATH|Playwright is not installed|is not installed|no free port') {
        Note "WARNING smoke: SKIPPED - $($setup[0])"
    } elseif ($setup.Count -or -not $runDir -or -not (Test-Path -LiteralPath (Join-Path $runDir 'findings.json'))) {
        Stop-Release "the smoke run could not start (exit $smokeCode):`n$(($smokeOut | Select-Object -Last 15) -join "`n")"
    } else {
        $found = @((Get-Content -LiteralPath (Join-Path $runDir 'findings.json') -Raw | ConvertFrom-Json).findings)
        $rows = @(); try { $rows = @(Get-Content -LiteralPath (Join-Path $runDir 'server-errors.json') -Raw | ConvertFrom-Json) } catch { }
        $stops = @($found | Where-Object { $_.severity -eq 'high' -and $_.kind -ne 'numbers' } | ForEach-Object { "$($_.role) | $($_.step) | $($_.kind): $($_.message)" })
        $stops += @($rows | Where-Object { $_.severity -eq 'PANIC' } | ForEach-Object { "server PANIC: $($_.module_source): $($_.error_message)" })
        $warns = @($found | Where-Object { $_.kind -eq 'numbers' -or $_.severity -eq 'medium' } | ForEach-Object { "$($_.step) | $($_.kind): $($_.message)" })
        $warns += @($rows | Where-Object { $_.severity -ne 'PANIC' } | ForEach-Object { "server $($_.severity): $($_.module_source): $($_.error_message)" })
        $report.Add(''); $report.Add("Browser smoke run (G10a) - Guide report: $runDir\report.html")
        ($stops + $warns) | Select-Object -First 40 | ForEach-Object { $report.Add("    $_") }
        if ($stops.Count) { Stop-Release "the browser smoke run found $($stops.Count) problem(s):`n$(($stops | Select-Object -First 10) -join "`n")`nReport: $runDir\report.html" }
        $smokeResult = "pass ($($warns.Count) warning(s))"
        Note "smoke: signed in, POS screen, one sale and the Sales Register - $($warns.Count) warning(s); Guide report $runDir\report.html"
    }
}

# ------------------------------------------------ G10b release-candidate gate
# Stage 47.17.3 / 49.17.2: one evidence record for this exact candidate (base commit +
# tree, migrations with checksums, SBOM, config contract, surface diff, the results
# G8/G10 just produced, the exceptions register, run evidence). cmd/releasegate holds
# the rules; release\gate-policy.json says which of them only warn. Written to %TEMP% and
# copied in (Controlled Folder Access), kept in reports\ with the release report.
Write-Gate 'G10b Release-candidate gate'
Invoke-Git @('add', '-A') $wt | Out-Null
$rcTree = (Invoke-Git @('write-tree') $wt)[0]
$rcOut = Join-Path $env:TEMP "release_rc_$ts"
$impactFile = Join-Path $env:TEMP "release_impact_$ts.txt"
$rcResults = if ($onGitHub) {
    # 177.2: build, vet and tests run on GitHub after the push; main moves only when they pass there.
    # 47.9.4 / 47.17.2 (ask 241): release.yml's evidence job runs the browser budgets and the
    # journeys on the candidate and judges them; promote needs it. The record says deferred.
    @('-result', 'build=github (release.yml, before main moves)', '-result', 'vet=github (release.yml, before main moves)', '-result', 'tests=github (release.yml: every package, 16 shards)', '-result', "smoke=$smokeResult", '-github-kinds', 'browser_budget,journey')
} else {
    @('-result', 'build=pass', '-result', 'vet=pass', '-result', "tests=pass ($ranCount engines+server tests, $($state.flaky.Count) flaky)", '-result', "smoke=$smokeResult")
}
$rcArgs = @('run', './cmd/releasegate', 'check', '-root', '.', '-base', $startHead, '-tree', $rcTree, '-out', $rcOut) + $rcResults
if (Test-Path -LiteralPath $impactFile) { $rcArgs += @('-impact-file', $impactFile) }
$rcOutLines = Invoke-Native -Exe $go -Arguments $rcArgs -WorkDir $wt -AllowFail
$rcCode = $LASTEXITCODE
$report.Add(''); $report.Add('Release-candidate gate (G10b):'); $rcOutLines | ForEach-Object { $report.Add("    $_") }
if (Test-Path -LiteralPath $rcOut) { Get-ChildItem -LiteralPath $rcOut | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $reportDir -Force } }
Remove-Item $impactFile, $rcOut -Recurse -ErrorAction SilentlyContinue
$rcWarn = @($rcOutLines | Where-Object { $_ -match '^(WARN|NOT-RUN)\s' }).Count
if ($rcCode -ne 0) { Stop-Release "release-candidate gate failed:`n$(($rcOutLines | Where-Object { $_ -match '^FAIL' }) -join "`n")`nRecord: $reportDir" }
Note "release-candidate record written to reports\ ($rcWarn warning/not-run - see the report; docs/operations/release-candidate-gate.md)"

if ($DryRun) {
    $state.status = 'dry-run passed'
    $files = Invoke-Git @('diff', '--cached', '--name-status', '--no-renames', 'HEAD') $wt
    $report.Add(''); $report.Add('Would commit:'); $files | ForEach-Object { $report.Add("    $_") }
    Save-Report
    Write-Host ''
    Write-Host "DRY RUN PASSED - every gate green; nothing committed. $($files.Count) file(s) would ship." -ForegroundColor Green
    Write-Host "Worktree: $wt"
    Write-Host "Report:   $reportPath"
    exit 0
}

# --------------------------------------------------------------- G11 commit
# Through the shared index, and only if the tree has not moved since the
# snapshot - another session's newer edit must never be overwritten.
Write-Gate 'G11 Commit'
Invoke-Git @('add', '-A') $wt | Out-Null
$tree = (Invoke-Git @('write-tree') $wt)[0]
$headTree = (Invoke-Git @('rev-parse', 'HEAD^{tree}') $wt)[0]
if ((Invoke-Git @('rev-parse', 'HEAD'))[0] -ne $startHead) { Stop-Release 'HEAD moved during the release (another session committed) - run Release again' }
if ((Invoke-Git @('diff', '--cached', '--name-only')) | Where-Object { $_ }) { Stop-Release 'the shared index gained staged files during the release - run Release again' }
$moved = @($snapHash.Keys | Where-Object { (Get-ContentHash (Join-Path $repo $_)) -ne $snapHash[$_] })
if ($moved.Count) { Stop-Release "files changed in the tree during the release: $(($moved | Select-Object -First 8) -join ', ') - run Release again" }
$newCommit = $startHead
if ($tree -ne $headTree) {
    # The tested blobs go into the shared index as arguments, not through a
    # PowerShell pipe: piping text into git re-encodes it ("malformed index
    # info", found by the sandbox run on 2026-10-19).
    $ns = Invoke-Git @('diff', '--cached', '--no-renames', '--name-status', 'HEAD') $wt
    $adds = [System.Collections.Generic.List[object]]::new(); $dels = [System.Collections.Generic.List[string]]::new()
    foreach ($l in $ns) {
        $st, $p = $l -split "`t", 2
        if ($st -eq 'D') { $dels.Add($p); continue }
        $meta = ((Invoke-Git @('ls-files', '-s', '--', $p) $wt)[0] -split "`t", 2)[0] -split ' '
        $adds.Add(@($meta[0], $meta[1], $p))
    }
    try {
        for ($i = 0; $i -lt $adds.Count; $i += 40) {
            $a = @('-C', $repo, 'update-index', '--add')
            foreach ($e in $adds[$i..([Math]::Min($i + 39, $adds.Count - 1))]) { $a += @('--cacheinfo', $e[0], $e[1], $e[2]) }
            Invoke-Native -Exe 'git' -Arguments $a | Out-Null
        }
        for ($i = 0; $i -lt $dels.Count; $i += 40) {
            Invoke-Native -Exe 'git' -Arguments (@('-C', $repo, 'update-index', '--force-remove', '--') + $dels[$i..([Math]::Min($i + 39, $dels.Count - 1))]) | Out-Null
        }
    } catch { Invoke-Git @('reset', '-q') | Out-Null; Stop-Release "git update-index refused the release entries; index reset, nothing committed: $($_.Exception.Message)" }
    $sharedTree = (Invoke-Git @('write-tree'))[0]
    if ($sharedTree -ne $tree) { Invoke-Git @('reset', '-q') | Out-Null; Stop-Release 'the staged tree did not match the tested tree; index reset, nothing committed' }

    # Message: the ledger sections this release adds, else the new asks in the checklist.
    # Since 2026-11-10 the checklist is arranged by module: a new ask adds a run header
    # (> **User, <date>: "<words>"**) to its Asks log instead of a "## Stage" heading.
    $titles = @((Invoke-Git @('diff', '--cached', 'HEAD', '--', 'docs/project_ledger.md') $wt) | Where-Object { $_ -match '^\+## \d+\. ' } | ForEach-Object { ($_ -replace '^\+## \d+\.\s*', '') -replace '\s*\(\d{4}-.*$', '' })
    if (-not $titles.Count) {
        $titles = @((Invoke-Git @('diff', '--cached', 'HEAD', '--', 'docs/micro_checklist.md') $wt) | ForEach-Object {
            if ($_ -match '^\+> \*\*User, \d{4}-\d{2}-\d{2}: "([^"]+)"') { $w = $Matches[1]; if ($w.Length -gt 80) { $w.Substring(0, 77) + '...' } else { $w } }
            elseif ($_ -match '^\+## Stage ') { ($_ -replace '^\+## ', '') -replace '\s*\(opened.*$', '' }
        } | Select-Object -Unique)
    }
    if (-not $titles.Count) {
        # No new ledger section or stage: name the checklist items this release adds or moves (0.6.0 said only "39 file(s)").
        $ids = @((Invoke-Git @('diff', '--cached', 'HEAD', '--', 'docs/micro_checklist.md') $wt) | ForEach-Object { if ($_ -match '^\+- \[[ x]\] \*\*([0-9][0-9A-Za-z.]*) ') { $Matches[1] } } | Select-Object -Unique)
        if ($ids.Count) { $titles = @('items ' + (($ids | Select-Object -First 12) -join ', ') + $(if ($ids.Count -gt 12) { " (+$($ids.Count - 12) more)" } else { '' })) }
    }
    # feat: / docs: so cmd/releasever counts this commit the way G4b did.
    $subject = if ($titles.Count) { "${releaseKind}: release $releaseVersion - " + (($titles | Select-Object -First 4) -join '; ') } else { "${releaseKind}: release $releaseVersion - $($ns.Count) file(s)" }
    if ($subject.Length -gt 200) { $subject = $subject.Substring(0, 197) + '...' }
    $body = @($subject, '')
    $body += $titles | ForEach-Object { "- $_" }
    $body += ''
    $body += if ($onGitHub) { "Gates passed (release/scripts/Invoke-Release.ps1): snapshot, LF, gofmt, cache versions, compliance reviews, catalog spec, attack surface, KB, brain, register, JS, budget. Build, vet, migrate from empty and the full tests run on GitHub (.github/workflows/release.yml) before main moves to this commit." }
              else { "Gates passed (release/scripts/Invoke-Release.ps1): snapshot, LF, gofmt, cache versions, compliance reviews, catalog spec, attack surface, KB, brain, register, build, vet, JS, budget, migrate from empty, full tests." }
    if ($state.flaky.Count) { $body += "Flaky (passed on rerun): $($state.flaky -join ', ')" }
    if ($held.Count) { $body += "Held back: $($held -join ', ')" }
    $msgFile = Join-Path $env:TEMP "release_msg_$ts.txt"
    Write-Text $msgFile (($body -join "`n") + "`n")
    Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'commit', '-q', '-F', $msgFile) | Out-Null
    $newCommit = (Invoke-Git @('rev-parse', 'HEAD'))[0]
    if ((Invoke-Git @('rev-parse', 'HEAD^{tree}'))[0] -ne $tree) { Stop-Release "committed $newCommit but its tree differs from the tested one - check before pushing" }
    Remove-Item $msgFile -ErrorAction SilentlyContinue
    Note "committed $($newCommit.Substring(0,7)): $subject"
} else { Note 'nothing new to commit (pushing earlier commits)' }
$state.commit = $newCommit; $state.tree = $tree

# ---------------------------------------------- G12 sync regenerated files back
# The shared tree gets the committed version of every file the release changed
# (version bumps, gofmt, regenerated docs) - but only where nobody touched the
# file since the snapshot, and never a held path.
Write-Gate 'G12 Sync the shared tree'
$synced = 0
foreach ($p in (Invoke-Git @('diff', '--name-only', $startHead, $newCommit))) {
    if (-not $p -or (Test-Held $p)) { continue }
    $dst = Join-Path $repo $p; $src = Join-Path $wt $p
    if (-not (Test-Path -LiteralPath $src)) { continue }
    $untouched = if ($snapHash.ContainsKey($p)) { (Get-ContentHash $dst) -eq $snapHash[$p] } else {
        # Still the old HEAD's content? git diff applies git's own line-ending
        # rules (a CRLF checkout, the i/mixed manifests), the way git status does.
        & git -C $repo diff --quiet $startHead -- $p 2>$null
        (-not (Test-Path -LiteralPath $dst)) -or ($LASTEXITCODE -eq 0)
    }
    if ($untouched -and (Get-ContentHash $dst) -ne (Get-ContentHash $src)) {
        New-Item -ItemType Directory -Force -Path (Split-Path $dst -Parent) | Out-Null
        Copy-Item -LiteralPath $src -Destination $dst -Force; $synced++
    }
}
# G12b (2026-10-26): a cache-version bump G4 made in a file the tree keeps its
# own copy of (held, or edited since the snapshot) is carried into that copy,
# that one line only. 0.17.0 bumped VIEW_MODULE_VERSION to 66 in a held app.js
# and the working copy stayed at 65 - its next release would have shipped 65
# again, and browsers would have kept the old screens.
$cacheLines = @(
    @{ File = 'public/app.js'; Re = "const VIEW_MODULE_VERSION = '(\d+)';"; Fmt = "const VIEW_MODULE_VERSION = '{0}';" },
    @{ File = 'public/index.html'; Re = 'app\.js\?v=(\d+)'; Fmt = 'app.js?v={0}' },
    @{ File = 'public/index.html'; Re = 'styles\.css\?v=(\d+)'; Fmt = 'styles.css?v={0}' },
    @{ File = 'public/index.html'; Re = 'qz-print\.js\?v=(\d+)'; Fmt = 'qz-print.js?v={0}' },
    @{ File = 'public/index.html'; Re = 'theme-boot\.js\?v=(\d+)'; Fmt = 'theme-boot.js?v={0}' },
    @{ File = 'public/index.html'; Re = 'erp-typeahead\.js\?v=(\d+)'; Fmt = 'erp-typeahead.js?v={0}' }
)
foreach ($c in $cacheLines) {
    $dst = Join-Path $repo $c.File; $src = Join-Path $wt $c.File
    if (-not (Test-Path -LiteralPath $dst) -or -not (Test-Path -LiteralPath $src)) { continue }
    $committed = [regex]::Match((Read-Text $src), $c.Re); $mine = [regex]::Match((Read-Text $dst), $c.Re)
    if (-not $committed.Success -or -not $mine.Success) { continue }
    if ([int]$mine.Groups[1].Value -lt [int]$committed.Groups[1].Value) {
        Write-Text $dst ([regex]::Replace((Read-Text $dst), $c.Re, ($c.Fmt -f $committed.Groups[1].Value)))
        Note "$($c.File): $($c.Fmt -f $mine.Groups[1].Value) -> $($c.Fmt -f $committed.Groups[1].Value) carried into the shared tree's own copy"
    }
}
& git -C $repo update-index -q --refresh 2>$null | Out-Null   # drop stat-only "modified" entries
Note "$synced file(s) synced; tree changes left: $((Invoke-Git @('status', '--porcelain')).Where({ $_ }).Count)"

# ---------------------------------------------------------------- G13 push
Write-Gate 'G13 Push'
if ($NoPush) { Note 'not pushed (-NoPush)'; $state.status = 'committed' }
elseif ($onGitHub) {
    # 177.2: push the release commit to candidate/<version>, not main. release.yml tests that
    # exact commit and fast-forwards main to it only when every job passes. Deploy waits for that.
    $candidate = "candidate/$releaseVersion"
    $push = Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'push', 'origin', "$($newCommit):refs/heads/$candidate") -AllowFail
    if ($LASTEXITCODE -ne 0) { $state.status = 'committed'; Stop-Release "push to $candidate failed:`n$(($push | Select-Object -Last 5) -join "`n")" }
    $state.status = 'committed'; $state.tests = 'github'; $state.candidate = $candidate
    Note "pushed to $candidate - GitHub now builds and tests it; main moves when every job passes"
    # The run appears a few seconds after the push.
    $runUrl = ''; $runId = ''
    for ($i = 0; $i -lt 15 -and -not $runId; $i++) {
        Start-Sleep -Seconds 2
        $runs = Invoke-Native -Exe 'gh' -Arguments @('run', 'list', '--workflow', 'release.yml', '--commit', $newCommit, '--limit', '1', '--json', 'databaseId,url') -WorkDir $repo -AllowFail
        if ($LASTEXITCODE -eq 0) { try { $r = @(($runs -join "`n") | ConvertFrom-Json); if ($r.Count) { $runId = [string]$r[0].databaseId; $runUrl = [string]$r[0].url } } catch { } }
    }
    $state.run = $runUrl
    Note $(if ($runUrl) { "GitHub run: $runUrl" } else { 'GitHub run: not listed yet - see the Actions tab' })
    if ($Wait -and $runId) {
        Save-Report
        Write-Host "   waiting for the GitHub run..."
        & gh run watch $runId --exit-status --interval 15 2>&1 | Select-Object -Last 3 | ForEach-Object { Write-Host "   $_" }
        if ($LASTEXITCODE -ne 0) { Stop-Release "the GitHub run failed - main did not move. See $runUrl" }
        & git -C $repo fetch -q origin main 2>$null
        $state.status = 'passed'
        Note 'GitHub run passed; main moved to the release commit'
    }
}
else {
    $push = Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'push', 'origin', 'main') -AllowFail
    if ($LASTEXITCODE -ne 0) { $state.status = 'committed'; Stop-Release "push failed:`n$(($push | Select-Object -Last 5) -join "`n")" }
    Note (($push | Select-Object -Last 1) -join '')
    $state.status = 'passed'
}
Save-Report
if (-not $KeepWorktree) {
    # 177.5: the exact code map is drawn now, in the background at low priority, from this
    # worktree (the release commit's files). The redraw keeps the graph for the next G7 and
    # removes the worktree; Deploy (D6) brings the map into the shared tree.
    $redrawScript = Join-Path $PSScriptRoot 'Invoke-BrainRedraw.ps1'
    Start-Process -FilePath 'pwsh' -ArgumentList "-NoProfile -File `"$redrawScript`" -Worktree `"$wt`" -Commit $newCommit" -WorkingDirectory $env:TEMP -WindowStyle Hidden
    Note "code map redraw started in the background ($BrainRedrawDir)"
}
Write-Host ''
if ($onGitHub -and $state.status -ne 'passed') {
    Write-Host "RELEASED $($newCommit.Substring(0,7)) to $($state.candidate) - GitHub tests it now; Deploy.cmd waits for that run" -ForegroundColor Green
} else {
    Write-Host "RELEASED $($newCommit.Substring(0,7)) - next: Deploy.cmd" -ForegroundColor Green
}
Write-Host "Report: $reportPath"
exit 0
} catch {
    Stop-Release "$($_.Exception.Message)"
} finally {
    # exit inside the try still runs this, so every way out of a release frees the lock
    if ($lockHeld) { Remove-Item -LiteralPath $lockPath -Force -ErrorAction SilentlyContinue }
}
