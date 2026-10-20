<#
Deploy click (Stage 109.2): puts the commit Release.cmd pushed onto
production, the way the 2026-10-18/19 deploys were done by hand.

  release\Deploy.cmd                              # deploy origin/main
  pwsh release\scripts\Invoke-Deploy.ps1 -Check   # read-only: what would change, nothing done
  pwsh release\scripts\Invoke-Deploy.ps1 -Yes     # no prompt (only for a scheduled run)

It refuses a commit that is not on origin/main or that Release.cmd did not
pass, shows every pending migration / backfill / background job that changes
client data and waits for YES, takes a backup (checked against its sha256)
and a rollback copy, deploys from a clean worktree of the commit with
deploy\deploy.ps1, and checks every host. remote_deploy.sh then runs one
server cleanup pass (Stage 109.3).
#>
param([switch]$Check, [switch]$Yes)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\ReleaseCommon.ps1"

$cfg = Get-ReleaseConfig
$repo = $RepoRoot
$ts = Get-Date -Format 'yyyyMMdd_HHmmss'
$reportDir = Join-Path $ReleaseDir 'reports'
New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
$reportPath = Join-Path $reportDir "deploy-$ts.md"
$report = [System.Collections.Generic.List[string]]::new()
$report.Add("# Deploy $ts")
$wt = ''

function Note([string]$line) { Write-Host "   $line"; $report.Add("- $line") }
function Stop-Deploy([string]$why) {
    Write-Host ''
    Write-Host "DEPLOY STOPPED: $why" -ForegroundColor Red
    $report.Add(''); $report.Add("**Stopped:** $why")
    $report | Set-Content -LiteralPath $reportPath -Encoding utf8
    Write-Host "Report: $reportPath"
    exit 1
}
function Invoke-Git([string[]]$a) { Invoke-Native -Exe 'git' -Arguments (@('-C', $repo) + $a) }
function Remote([string]$cmd) {
    $out = @(& ssh -o ConnectTimeout=20 -o BatchMode=yes $cfg.target $cmd 2>&1 | ForEach-Object { "$_" })
    if ($LASTEXITCODE -ne 0) { throw "on the server: $cmd`n$(($out | Select-Object -Last 10) -join "`n")" }
    return , $out
}
function Get-Live([string]$h) {
    try { return (Invoke-RestMethod -Uri "https://$h/api/v1/version" -TimeoutSec 20) } catch { return $null }
}

try {
# ---------------------------------------------------------------- D0 preflight
Write-Gate 'D0 What is being deployed'
Invoke-Git @('fetch', '-q', 'origin') | Out-Null
$target = (Invoke-Git @('rev-parse', 'origin/main'))[0]
$short = $target.Substring(0, 7)
$localOnly = [int](Invoke-Git @('rev-list', '--count', 'origin/main..main'))[0]
if ($localOnly -gt 0) { Stop-Deploy "main has $localOnly commit(s) that are not on origin - run Release.cmd first (never deploy what GitHub does not have)" }
$lastPath = Join-Path $reportDir 'last-release.json'
$last = if (Test-Path $lastPath) { Get-Content $lastPath -Raw | ConvertFrom-Json } else { $null }
if (-not $last -or $last.commit -ne $target -or $last.status -ne 'passed') {
    $why = "origin/main is $short but the last passed Release.cmd run is $(if ($last -and $last.commit) { "$($last.commit.Substring(0,7)) ($($last.status))" } else { 'none on this machine' }) - run Release.cmd so this exact commit goes through the gates"
    if (-not $Check) { Stop-Deploy $why }
    Note "NOTE (would stop a real deploy): $why"
} else { Note "origin/main $short passed Release.cmd at $($last.at)" }
$live = Get-Live $cfg.hosts[0]
if (-not $live) {
    $live = (Remote "curl -s -m 10 http://127.0.0.1:8080/api/v1/version") -join '' | ConvertFrom-Json
}
$liveCommit = "$($live.git_commit)"
Note "live: $($live.version) ($liveCommit)"
if ($target.StartsWith($liveCommit) -and $liveCommit.Length -ge 7) {
    Write-Host "Already live: $short is what production runs." -ForegroundColor Green
    if (-not $Check) { exit 0 }   # -Check still reports below whether the server config matches the repo
}
& git -C $repo cat-file -e "$liveCommit^{commit}" 2>$null
if ($LASTEXITCODE -ne 0) { Stop-Deploy "the live commit $liveCommit is not in this repo - fetch it, or check what production runs" }

# ------------------------------------------------- D1 what changes client data
Write-Gate 'D1 What this deploy changes in client data'
$newMig = @((Invoke-Git @('diff', '--name-only', '--diff-filter=A', $liveCommit, $target, '--', 'db/')) | Where-Object { $_ -like '*.sql' })
$tmpDir = Join-Path $env:TEMP "deploy_scan_$ts"
New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
$files = foreach ($m in $newMig) {
    $p = Join-Path $tmpDir (Split-Path $m -Leaf)
    [IO.File]::WriteAllText($p, ((& git -C $repo show "$($target):$m") -join "`n"))
    $p
}
$goDiff = Invoke-Git @('diff', $liveCommit, $target, '--', '*.go')
$findings = Get-DataChangeFindings -MigrationFiles @($files) -AddedGoLines @(Get-AddedGoLines $goDiff)
Remove-Item $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
Note "$($newMig.Count) migration(s) not yet on production"
Format-Findings $findings | ForEach-Object { Note $_ }
$serious = @($findings | Where-Object { $_.Kind -ne 'metadata' })
$disk = (Remote "df -P / | awk 'NR==2{print `$5}'")[0]
Note "server disk: $disk used"
# Server configuration the deploy keeps in step (Stage 109.11): the droplet ran
# the first baseline systemd unit for weeks while the repo's sandbox sat unused.
$unitRepo = ((& git -C $repo show "$($target):deploy/erp.service") -join "`n").Trim()
$unitLive = ((Remote "cat /etc/systemd/system/erp.service") -join "`n").Trim()
if ($unitRepo -eq $unitLive) { Note 'service file: the live one matches the repo' }
else { Note 'service file: differs from the repo - this deploy installs it (and puts the old one back by itself if the ERP is not healthy on it)' }
if ($Check) {
    $report | Set-Content -LiteralPath $reportPath -Encoding utf8
    Write-Host ''
    Write-Host "CHECK ONLY - nothing deployed. Report: $reportPath" -ForegroundColor Green
    exit 0
}
if ($serious.Count -and -not $Yes) {
    Write-Host ''
    Write-Host "The deploy will make the $($serious.Count) change(s) above to app and minn data." -ForegroundColor Yellow
    $answer = Read-Host 'Type YES to deploy, anything else to stop'
    if ($answer -cne 'YES') { Stop-Deploy 'not confirmed' }
    Note 'data changes confirmed (YES)'
}

# --------------------------------------------------- D2 backup + rollback copy
Write-Gate 'D2 Backup and rollback copy'
$rd = $cfg.remoteDir
$b = Remote "cd $rd && set -a && . /etc/erp/erp.env && set +a && sudo -u erp -E bash deploy/backup.sh 2>&1 | tail -n 3"
$dump = ($b | ForEach-Object { if ($_ -match 'backup complete:\s*(\S+)') { $Matches[1] } }) | Select-Object -Last 1
if (-not $dump) { Stop-Deploy "backup did not complete:`n$($b -join "`n")" }
$sum = Remote "cd `$(dirname $dump) && sha256sum -c `$(basename $dump).sha256"
if (-not ($sum -join ' ').Contains(': OK')) { Stop-Deploy "backup checksum did not verify: $($sum -join ' ')" }
Note "backup $dump (sha256 OK)"
$rb = "rollback_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
Remote "cd $rd && mkdir -p $rb && cp -a erp-server public db $rb/" | Out-Null
Note "rollback copy $rd/$rb"

# ---------------------------------------------------------------- D3 deploy
Write-Gate "D3 Deploy $short"
$wt = Join-Path $env:TEMP "erp_deploy_$short`_$ts"
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, '-c', 'core.autocrlf=false', 'worktree', 'add', '--detach', $wt, $target) | Out-Null
# Shown live and kept: deploy.ps1 writes with Write-Host (stream 6).
# The number Release.cmd wrote into internal/server/VERSION (gate G4b) is the
# number production reports - one number in the binary, the docs and the ledger.
$relVer = ((& git -C $repo show "$($target):internal/server/VERSION") -join '').Trim()
if ($relVer -notmatch '^\d+\.\d+\.\d+$') { Stop-Deploy "internal/server/VERSION at $short is '$relVer', not a release number" }
Note "release $relVer"
$out = @(& (Join-Path $wt 'deploy\deploy.ps1') -Target $cfg.target -Version $relVer -ReleaseNote "Release.cmd $short from $($live.version) ($liveCommit)" *>&1 | ForEach-Object { $s = "$_"; Write-Host "   $s"; $s })
$out | Where-Object { $_ -match 'Release |\[apply\]|Applied|\[sync|\[cost-center\]|WARN|DEPLOY-|recorded|actions=' } | ForEach-Object { $report.Add("- $_") }
if (-not ($out | Where-Object { $_.Trim() -eq 'DEPLOY-OK' })) { Stop-Deploy "deploy.ps1 did not report DEPLOY-OK (production stays on the previous build, or rolled back):`n$(($out | Select-Object -Last 15) -join "`n")" }

# ---------------------------------------------------------------- D4 verify
Write-Gate 'D4 Every host answers with the new build'
$ver = [regex]::Match(((& git -C $repo show "$($target):public/app.js") -join "`n"), "VIEW_MODULE_VERSION = '(\d+)'").Groups[1].Value
# Caddy answers 503 for the first seconds after the restart (0.9.0: every host 503
# at D4, all 200 twenty seconds later), so a host gets up to 90 s before it counts.
$bad = @()
foreach ($h in $cfg.hosts) {
    $until = (Get-Date).AddSeconds(90)
    do {
        $hostBad = @()
        foreach ($path in @('/', '/api/v1/health', "/app.js?v=$ver")) {
            $r = Invoke-WebRequest -Uri "https://$h$path" -TimeoutSec 20 -SkipHttpErrorCheck -UseBasicParsing
            if ($r.StatusCode -ne 200) { $hostBad += "$h$path -> $($r.StatusCode)" }
        }
        $v = Get-Live $h
        if (-not $v -or -not $target.StartsWith("$($v.git_commit)")) { $hostBad += "$h reports $($v.git_commit)" }
        if ($hostBad.Count -and (Get-Date) -lt $until) { Start-Sleep -Seconds 5 }
    } while ($hostBad.Count -and (Get-Date) -lt $until)
    if ($hostBad.Count) { $bad += $hostBad } else { Note "$h  200  $($v.version) ($($v.git_commit))" }
}
if ($bad.Count) { Stop-Deploy "deployed, but: $($bad -join '; ') - check the site now (rollback copy: $rd/$rb)" }
$disk = (Remote "df -P / | awk 'NR==2{print `$5}'")[0]
Note "server disk after cleanup: $disk"
$report | Set-Content -LiteralPath $reportPath -Encoding utf8
Invoke-Native -Exe 'git' -Arguments @('-C', $repo, 'worktree', 'remove', '--force', $wt) -AllowFail | Out-Null
Write-Host ''
Write-Host "DEPLOYED $short to $($cfg.hosts -join ', ')" -ForegroundColor Green
Write-Host "Report: $reportPath"
exit 0
} catch {
    Stop-Deploy "$($_.Exception.Message)"
}
