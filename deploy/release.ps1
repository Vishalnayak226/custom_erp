<#
Release click (Stage 109.1): every gate a release needs before origin/main
moves - the routine the 2026-10-18/19 releases (840909b, 0.2.0-0.4.0) ran by
hand, in the same order. It commits and pushes only when every gate passes.

  Release.cmd                                  # gates, commit, push
  pwsh deploy\release.ps1 -DryRun              # gates only; nothing committed
  pwsh deploy\release.ps1 -NoPush              # gates + local commit
  pwsh deploy\release.ps1 -QuietMinutes 0      # skip the "tree is quiet" wait

What ships: everything modified or new in the working tree, minus the paths
in deploy\release-hold.txt (git-ignored; one path or glob per line, `#` starts
a comment). A session whose work must not ship yet lists its paths there.

A release problem this script did not catch is a missing gate: add it here and
to the gate table in deploy\README.md in the same session (CLAUDE.md rule).
#>
param(
    [switch]$DryRun,
    [switch]$NoPush,
    [int]$QuietMinutes = -1,
    [switch]$KeepWorktree
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\release_common.ps1"

$cfg = Get-ReleaseConfig
$repo = Split-Path $PSScriptRoot -Parent
$go = $cfg.goBin
$goDir = Split-Path $go -Parent
$gofmt = Join-Path $goDir 'gofmt.exe'
if ($QuietMinutes -lt 0) { $QuietMinutes = [int]$cfg.quietMinutes }
$ts = Get-Date -Format 'yyyyMMdd_HHmmss'
$reportDir = Join-Path $PSScriptRoot 'release-reports'
New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
$reportPath = Join-Path $reportDir "release-$ts.md"
$report = [System.Collections.Generic.List[string]]::new()
$report.Add("# Release $ts")
$wt = Join-Path $env:TEMP "erp_release_$ts"
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

try {
# ---------------------------------------------------------------- G0 preflight
Write-Gate 'G0 Preflight'
foreach ($tool in @('git', 'node', 'pwsh', 'graphify')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Stop-Release "$tool is not on PATH" }
}
foreach ($p in @($go, $gofmt, $cfg.psql)) { if (-not (Test-Path $p)) { Stop-Release "not found: $p (deploy\release.config.json)" } }
$branch = (Invoke-Git @('rev-parse', '--abbrev-ref', 'HEAD'))[0]
if ($branch -ne 'main') { Stop-Release "the repo is on '$branch', not main" }
if (@((Invoke-Git @('diff', '--cached', '--name-only')) | Where-Object { $_ }).Count) { Stop-Release 'something is already staged in the index - a session is mid-commit; let it finish' }
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

$holdFile = Join-Path $PSScriptRoot 'release-hold.txt'
$holds = @()
if (Test-Path $holdFile) {
    $holds = Get-Content $holdFile | ForEach-Object { ($_ -replace '#.*$', '').Trim() } | Where-Object { $_ }
}
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
foreach ($c in $ship | Where-Object { $_.Code -ne ' D' }) {
    $p = Join-Path $wt $c.Path
    if ($mixed -contains $c.Path -or (Test-Binary $p)) { continue }
    # Files .gitattributes keeps CRLF (the .cmd launchers) stay as they are.
    if ((& git -C $wt check-attr eol -- $c.Path) -match ':\s*crlf$') { continue }
    $s = Read-Text $p
    if ($s.Contains("`r`n")) { Write-Text $p ($s.Replace("`r`n", "`n")); $fixed++ }
}
Note "$fixed file(s) converted to LF"

# ---------------------------------------------------------------- G3 gofmt
Write-Gate 'G3 gofmt'
foreach ($c in $ship | Where-Object { $_.Code -ne ' D' -and $_.Path -like '*.go' }) {
    $p = Join-Path $wt $c.Path
    $bad = Invoke-Native -Exe $gofmt -Arguments @('-l', $p) -AllowFail
    if (-not ($bad | Where-Object { $_ })) { continue }
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
} else { Note 'no public/ change' }

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
$codeShips = @($ship | Where-Object { $_.Path -notlike 'docs/*' -and $_.Path -notlike '*.md' }).Count -gt 0
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

# ------------------------------------------------- G5 generated Go and reports
Write-Gate 'G5 Report catalog spec'
if (Test-Path (Join-Path $wt 'cmd/gencatalog')) {
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
Copy-Item $surfTmp (Join-Path $wt 'docs/security/attack_surface.json') -Force
Remove-Item $surfTmp -ErrorAction SilentlyContinue
Invoke-Native -Exe $go -Arguments @('run', './cmd/surfacescan', '-check') -WorkDir $wt | Out-Null
Note 'attack_surface.json regenerated from the release set'

# ---------------------------------------------------------------- G7 docs
Write-Gate 'G7 Guides, KB, brain, register'
$kb = Invoke-Docs 'KB'
if (-not $kb.ok) { Stop-Release "update-docs -Group KB failed:`n$(($kb.out | Select-Object -Last 15) -join "`n")" }
Note (($kb.out | Where-Object { $_ -match 'Published' }) -join ' ')
Invoke-Native -Exe 'graphify' -Arguments @('update', '.') -WorkDir $wt | Out-Null
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
Note 'brain redrawn (100% coverage)'
$regTmp = Join-Path $env:TEMP "release_register_$ts.json"
Invoke-Native -Exe $go -Arguments @('run', './cmd/doclint', '-root', '.', '-write-register', $regTmp) -WorkDir $wt -AllowFail | Out-Null
if (-not (Test-Path $regTmp)) { Stop-Release 'doclint could not write the document register' }
Copy-Item $regTmp (Join-Path $wt 'docs/governance/document-register.json') -Force
Remove-Item $regTmp -ErrorAction SilentlyContinue
$lint = Invoke-Native -Exe $go -Arguments @('run', './cmd/doclint', '-root', '.') -WorkDir $wt -AllowFail
$lintBad = @($lint | Where-Object { $_ -match 'unregistered|missing registered|broken' })
if ($lintBad.Count) { Stop-Release "doclint: $($lintBad.Count) broken link / register problem(s):`n$(($lintBad | Select-Object -First 10) -join "`n")" }
Note 'document register rewritten; no broken links'

# ---------------------------------------------------------------- G8 static
Write-Gate 'G8 Build, vet, JS, budget'
Invoke-Native -Exe $go -Arguments @('build', './...') -WorkDir $wt | Out-Null
Invoke-Native -Exe $go -Arguments @('vet', './...') -WorkDir $wt | Out-Null
Note 'go build + go vet clean'
$jsFiles = @($ship | Where-Object { $_.Code -ne ' D' -and $_.Path -like '*.js' } | ForEach-Object Path) + @('public/app.js') | Select-Object -Unique
foreach ($js in $jsFiles) { Invoke-Native -Exe 'node' -Arguments @('--check', (Join-Path $wt $js)) | Out-Null }
Note "node --check: $($jsFiles.Count) file(s)"
$budget = Invoke-Native -Exe $go -Arguments @('run', './cmd/releasebudget') -WorkDir $wt -AllowFail
$budgetText = $budget -join "`n"
if ($budgetText -match '"status":\s*"over') {
    $over = [regex]::Matches($budgetText, '"(\w+)":\s*\{[^{}]*"value":\s*(\d+)[^{}]*"limit":\s*(\d+)[^{}]*"status":\s*"over_budget"') | ForEach-Object { "$($_.Groups[1].Value) $($_.Groups[2].Value) > $($_.Groups[3].Value)" }
    Stop-Release "release budget over: $($over -join '; '). The cold core counts view-pos.js too - move new code into a lazily loaded module"
}
Note 'release budget within limits'

# ---------------------------------------------------- G9 data-change scan
Write-Gate 'G9 What this changes in client data'
Invoke-Git @('add', '-A') $wt | Out-Null
$newMigrations = @((Invoke-Git @('diff', '--cached', '--name-only', '--diff-filter=A', 'HEAD', '--', 'db/') $wt) | Where-Object { $_ -like '*.sql' } | ForEach-Object { Join-Path $wt $_ })
$goDiff = Invoke-Git @('diff', '--cached', 'HEAD', '--', '*.go') $wt
$findings = Get-DataChangeFindings -MigrationFiles $newMigrations -AddedGoLines @(Get-AddedGoLines $goDiff)
$state.findings = @($findings | ForEach-Object { "[$($_.Kind)] $($_.File):$($_.Line) $($_.Text)" })
Format-Findings $findings | ForEach-Object { Note $_ }
Note "$($newMigrations.Count) new migration(s); the Deploy click asks before running any [client data] / [DESTRUCTIVE] change"

# ------------------------------------------------- G10 fresh database + tests
# Every migration from empty (the shared custom_erp hides ordering and
# from-zero bugs), then the whole suite on that database, one package at a time.
Write-Gate 'G10 Fresh database, migrations, full tests'
Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + "CREATE DATABASE $scratchDb") | Out-Null
$dbUrl = "postgres://$($cfg.pgUser)@$($cfg.pgHost):$($cfg.pgPort)/$($scratchDb)?sslmode=disable"
$serverExe = Join-Path $env:TEMP "release_erp_server_$ts.exe"
Invoke-Native -Exe $go -Arguments @('build', '-o', $serverExe, './cmd/server') -WorkDir $wt | Out-Null
$env:DATABASE_URL = $dbUrl; $env:TEST_DATABASE_URL = $dbUrl
try {
    $mig = Invoke-Native -Exe $serverExe -Arguments @('-migrate') -WorkDir $env:TEMP -AllowFail
    if ($LASTEXITCODE -ne 0) { Stop-Release "migrate from empty failed:`n$(($mig | Select-Object -Last 15) -join "`n")" }
    if ($mig | Where-Object { $_ -match '\[sync-warn\]' }) { Stop-Release "migrate printed [sync-warn]:`n$(($mig | Where-Object { $_ -match 'sync-warn' }) -join "`n")" }
    Note (($mig | Where-Object { $_ -match '^Applied' }) -join ' ')
    $testLog = Join-Path $reportDir "release-$ts-tests.log"
    Write-Host '   running the full test suite (about 10-15 minutes)...'
    $tests = Invoke-Native -Exe $go -Arguments @('test', './...', '-p', '1', '-count=1', '-timeout', $cfg.testTimeout) -WorkDir $wt -AllowFail
    $tests | Set-Content -LiteralPath $testLog -Encoding utf8
    if ($LASTEXITCODE -ne 0) {
        if ($tests | Where-Object { $_ -match '\[build failed\]|^panic:|setup failed' }) { Stop-Release "tests did not build or panicked - see $testLog" }
        # Which tests failed, in which package.
        $failed = [System.Collections.Generic.List[object]]::new(); $pending = @()
        foreach ($l in $tests) {
            if ($l -match '^\s*--- FAIL: (\w+)') { $pending += $Matches[1] }
            elseif ($l -match '^(FAIL|ok)\s+(custom_erp/\S+)') { foreach ($t in $pending | Select-Object -Unique) { $failed.Add([pscustomobject]@{ Pkg = $Matches[2]; Test = $t }) }; $pending = @() }
        }
        if (-not $failed.Count) { Stop-Release "tests failed without a named test - see $testLog" }
        $still = @()
        foreach ($f in $failed) {
            $passed = $false
            for ($r = 1; $r -le [int]$cfg.flakyReruns -and -not $passed; $r++) {
                Invoke-Native -Exe $go -Arguments @('test', $f.Pkg, '-run', "^$($f.Test)$", '-count=1') -WorkDir $wt -AllowFail | Out-Null
                $passed = ($LASTEXITCODE -eq 0)
            }
            if ($passed) { $state.flaky += "$($f.Pkg) $($f.Test)"; Note "flaky (failed once, passed on rerun): $($f.Test)" } else { $still += "$($f.Pkg) $($f.Test)" }
        }
        if ($still.Count) { Stop-Release "tests fail: $($still -join ', ') - see $testLog" }
    }
    Note "full test suite green ($(@($tests | Where-Object { $_ -match '^ok\s' }).Count) packages)"
} finally {
    Remove-Item Env:\DATABASE_URL, Env:\TEST_DATABASE_URL -ErrorAction SilentlyContinue
    Remove-Item $serverExe -ErrorAction SilentlyContinue
}
Invoke-Native -Exe $cfg.psql -Arguments ($pgArgs + "DROP DATABASE IF EXISTS $scratchDb") -AllowFail | Out-Null

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

    # Message: the ledger sections this release adds, else the new checklist stages.
    $titles = @((Invoke-Git @('diff', '--cached', 'HEAD', '--', 'docs/project_ledger.md') $wt) | Where-Object { $_ -match '^\+## \d+\. ' } | ForEach-Object { ($_ -replace '^\+## \d+\.\s*', '') -replace '\s*\(\d{4}-.*$', '' })
    if (-not $titles.Count) { $titles = @((Invoke-Git @('diff', '--cached', 'HEAD', '--', 'docs/micro_checklist.md') $wt) | Where-Object { $_ -match '^\+## Stage ' } | ForEach-Object { ($_ -replace '^\+## ', '') -replace '\s*\(opened.*$', '' }) }
    # feat: / docs: so cmd/releasever counts this commit the way G4b did.
    $subject = if ($titles.Count) { "${releaseKind}: release $releaseVersion - " + (($titles | Select-Object -First 4) -join '; ') } else { "${releaseKind}: release $releaseVersion - $($ns.Count) file(s)" }
    if ($subject.Length -gt 200) { $subject = $subject.Substring(0, 197) + '...' }
    $body = @($subject, '')
    $body += $titles | ForEach-Object { "- $_" }
    $body += ''
    $body += "Gates passed (deploy/release.ps1): snapshot, LF, gofmt, cache versions, catalog spec, attack surface, KB, brain, register, build, vet, JS, budget, migrate from empty, full tests."
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
& git -C $repo update-index -q --refresh 2>$null | Out-Null   # drop stat-only "modified" entries
Note "$synced file(s) synced; tree changes left: $((Invoke-Git @('status', '--porcelain')).Where({ $_ }).Count)"

# ---------------------------------------------------------------- G13 push
Write-Gate 'G13 Push'
if ($NoPush) { Note 'not pushed (-NoPush)'; $state.status = 'committed' }
else {
    $push = Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'push', 'origin', 'main') -AllowFail
    if ($LASTEXITCODE -ne 0) { $state.status = 'committed'; Stop-Release "push failed:`n$(($push | Select-Object -Last 5) -join "`n")" }
    Note (($push | Select-Object -Last 1) -join '')
    $state.status = 'passed'
}
Save-Report
if (-not $KeepWorktree) { Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null }
Write-Host ''
Write-Host "RELEASED $($newCommit.Substring(0,7)) - next: Deploy.cmd" -ForegroundColor Green
Write-Host "Report: $reportPath"
exit 0
} catch {
    Stop-Release "$($_.Exception.Message)"
}
