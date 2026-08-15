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
& scp -r "$RepoRoot\public" "${Target}:$RemoteDir/"
if ($LASTEXITCODE -ne 0) { throw "scp of public/ failed" }
& scp -r "$RepoRoot\db" "${Target}:$RemoteDir/"
if ($LASTEXITCODE -ne 0) { throw "scp of db/ failed" }

Write-Host "Migrating + swapping binary + restarting on the box..." -ForegroundColor Cyan
# Migrations are run with the NEWLY uploaded binary, not the running one:
# since Stage 30.2.2 the migration files are embedded in the binary
# (db/migrate.go), so the old binary would apply the old set - exactly the
# migrations that are already applied. Still before the swap, so the new
# binary never serves a request against a schema it hasn't migrated.
$remote = @"
set -e
# `set -a` around the source is load-bearing: /etc/erp/erp.env is a systemd
# EnvironmentFile, so its lines are bare KEY=value with no `export`. A plain
# `source` therefore makes DATABASE_URL a shell variable that is NOT inherited
# by migrate.sh (a child process), and migrate.sh dies on its own
# `\${DATABASE_URL:?}` guard. Verified the hard way during the 2026-08-04 deploy.
set -a; source /etc/erp/erp.env; set +a
chmod +x $RemoteDir/erp-server.new
ERP_BINARY=$RemoteDir/erp-server.new bash $RemoteDir/deploy/migrate.sh
mv $RemoteDir/erp-server.new $RemoteDir/erp-server
chmod +x $RemoteDir/erp-server
sudo systemctl restart erp
sleep 2
systemctl is-active erp
echo DEPLOY-OK
"@
# Force LF. A PowerShell here-string carries THIS FILE's line endings, and a
# fresh git checkout on Windows gives it CRLF -- so every line arrives at bash
# with a trailing \r and the whole block fails in ways that read like nonsense:
# `set: -: invalid option`, `chmod: cannot access '/opt/erp/erp-server.new'$'\r'`,
# `Failed to restart erp\x0d.service`. It never bit before because this repo's
# working copy has always held LF (git only converts on checkout, and these
# files were written, not checked out) -- it bit the first deploy run from a
# clean worktree, 2026-08-15.
$remote = $remote -replace "`r`n", "`n"

$remoteOut = & ssh $Target $remote 2>&1
$remoteOut | ForEach-Object { Write-Host $_ }
if ($LASTEXITCODE -ne 0) { throw "remote migrate/restart failed" }
# The exit code alone is not enough. The CRLF failure above exited 0 while
# migrating nothing, swapping nothing and restarting nothing, and this script
# then printed "Deployed" over the top of it -- a deploy that reports success
# and changed nothing is worse than one that fails loudly. So the marker the
# remote block prints last has to actually be there.
if ($remoteOut -notcontains "DEPLOY-OK") { throw "remote block did not reach its end marker -- nothing was migrated or restarted. Output above." }

Write-Host "Deployed $commit to $Target." -ForegroundColor Green
