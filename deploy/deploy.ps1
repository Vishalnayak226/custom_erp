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
$remote = @"
set -e
source /etc/erp/erp.env
bash $RemoteDir/deploy/migrate.sh
mv $RemoteDir/erp-server.new $RemoteDir/erp-server
chmod +x $RemoteDir/erp-server
sudo systemctl restart erp
sleep 2
systemctl is-active erp
"@
& ssh $Target $remote
if ($LASTEXITCODE -ne 0) { throw "remote migrate/restart failed" }

Write-Host "Deployed $commit to $Target." -ForegroundColor Green
