<#
Build the Linux ERP binary on this Windows dev machine and ship it to the
Droplet, then migrate + restart. The Linux counterpart of promote.ps1's
build -> ship -> migrate -> restart loop, for redeploys after the one-time
box setup (see deploy/README.md).

Prereqs on the box (done once during setup, per README):
  - SSH key auth for the deploy user (so scp/ssh don't prompt for a password)
  - /opt/erp exists and is owned by the deploy user
  - deploy/ scripts, /etc/erp/erp.env, and the systemd unit are already in place
  - passwordless sudo for the restart, e.g. in /etc/sudoers.d/erp-deploy:
      <deployuser> ALL=(root) NOPASSWD: /bin/systemctl restart erp

Usage:
  .\deploy\deploy.ps1 -Target erp@203.0.113.10
  .\deploy\deploy.ps1 -Target erp@erp.yourdomain.com -RemoteDir /opt/erp
#>
param(
    [Parameter(Mandatory)][string]$Target,       # user@host
    [string]$RemoteDir = "/opt/erp"
)
$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path $PSScriptRoot -Parent
$GoBin = "$env:USERPROFILE\go-portable\go\bin\go.exe"
if (-not (Test-Path $GoBin)) { throw "go.exe not found at $GoBin" }

$commit    = (& git -C $RepoRoot rev-parse --short HEAD).Trim()
$buildTime = (Get-Date -AsUTC -Format "yyyy-MM-ddTHH:mm:ssZ")
$ldflags   = "-s -w -X custom_erp/internal/server.gitCommit=$commit -X custom_erp/internal/server.buildTime=$buildTime"

$buildDir = Join-Path $PSScriptRoot "build"
New-Item -ItemType Directory -Force -Path $buildDir | Out-Null
$out = Join-Path $buildDir "erp-server"

Write-Host "Cross-compiling erp-server (linux/amd64) @ $commit..." -ForegroundColor Cyan
$env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
Push-Location $RepoRoot
try {
    & $GoBin build -ldflags="$ldflags" -o $out ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
    Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}

Write-Host "Shipping binary, public/, and db/ to $Target`:$RemoteDir ..." -ForegroundColor Cyan
# Upload the new binary under a temp name so a failed transfer never leaves a
# half-copied binary in place of the running one.
& scp $out "${Target}:$RemoteDir/erp-server.new"
if ($LASTEXITCODE -ne 0) { throw "scp of binary failed" }
# Stage 50/BLD-005: this used to scp straight over the LIVE $RemoteDir/public,
# overwriting the running frontend before remote_deploy.sh ever got a chance
# to snapshot the pre-deploy one for rollback - its "pre-deploy snapshot" was
# actually a snapshot of the NEW release, so a rollback never actually
# restored the old frontend (confirmed via docs/qa/audit-deploy-mock.py:
# release_consistent was false on every failure case). Ship to a staging
# name instead, exactly like erp-server.new already does, so the real
# activation - and the real snapshot before it - happens inside
# remote_deploy.sh alongside the binary swap. A stale public.new from an
# interrupted previous attempt is removed first so scp -r recreates it fresh
# rather than nesting a copy inside it.
& ssh $Target "rm -rf $RemoteDir/public.new"
if ($LASTEXITCODE -ne 0) { throw "could not clear stale public.new on remote" }
& scp -r "$RepoRoot\public" "${Target}:$RemoteDir/public.new"
if ($LASTEXITCODE -ne 0) { throw "scp of public/ failed" }
& scp -r "$RepoRoot\db" "${Target}:$RemoteDir/"
if ($LASTEXITCODE -ne 0) { throw "scp of db/ failed" }
# Shipped fresh every deploy, not assumed pre-installed like migrate.sh --
# this carries the auto-rollback safety net itself, so a stale remote copy
# missing a fix would silently defeat the whole point.
& ssh $Target "mkdir -p $RemoteDir/deploy"
if ($LASTEXITCODE -ne 0) { throw "could not create $RemoteDir/deploy on remote" }
& scp "$RepoRoot\deploy\remote_deploy.sh" "${Target}:$RemoteDir/deploy/remote_deploy.sh"
if ($LASTEXITCODE -ne 0) { throw "scp of deploy/remote_deploy.sh failed" }

Write-Host "Migrating + swapping binary + health-checking + restarting on the box..." -ForegroundColor Cyan
# The actual migrate/swap/restart/health-check/rollback logic lives in
# deploy/remote_deploy.sh, not here -- see that file's header for why (in
# short: bash logic inside a PowerShell here-string bit this script twice
# already, via CRLF line endings and via `$var` being eaten by PowerShell
# interpolation before bash ever saw it; a real LF-normalized file sidesteps
# both). This wrapper only has to get two things right: `set -a` around the
# env source (a systemd EnvironmentFile has no `export`, so a plain `source`
# leaves DATABASE_URL as a shell variable the child process never inherits --
# verified the hard way during the 2026-08-04 deploy) and forcing LF on what
# little bash text remains here.
$remote = @"
set -e
set -a; source /etc/erp/erp.env; set +a
REMOTE_DIR=$RemoteDir DEPLOY_COMMIT=$commit bash $RemoteDir/deploy/remote_deploy.sh
"@
$remote = $remote -replace "`r`n", "`n"

$remoteOut = & ssh $Target $remote 2>&1
$remoteOut | ForEach-Object { Write-Host $_ }
# remote_deploy.sh's exit code is authoritative (ssh propagates it), but the
# end-state marker it prints last is what tells us WHICH failure this was --
# a deploy that reports success and changed nothing is worse than one that
# fails loudly, so this branches on the marker rather than trusting a 0 exit
# code alone.
if ($remoteOut -contains "DEPLOY-OK") {
    Write-Host "Deployed $commit to $Target." -ForegroundColor Green
} elseif ($remoteOut -contains "DEPLOY-ROLLED-BACK") {
    throw "Deploy of $commit FAILED its health check and was automatically rolled back -- $Target is back on the previous build and confirmed healthy. See the journalctl tail above for why the new build didn't come up."
} elseif ($remoteOut -contains "DEPLOY-ROLLBACK-FAILED") {
    throw "CRITICAL: deploy of $commit failed its health check AND the automatic rollback did not restore a healthy service on $Target. This needs a human on the box right now -- see output above."
} else {
    throw "remote deploy script did not reach any recognized end state (DEPLOY-OK / DEPLOY-ROLLED-BACK / DEPLOY-ROLLBACK-FAILED) -- nothing confirmed. Output above."
}
