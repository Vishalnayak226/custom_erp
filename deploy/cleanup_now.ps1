<#
Cleanup click (Stage 109.3): runs the server cleanup now instead of waiting
for the 03:30 cron. Same script, same rules (deploy/server_cleanup.sh).

  Cleanup.cmd                          # clean now
  pwsh deploy\cleanup_now.ps1 -DryRun  # say what it would remove
#>
param([switch]$DryRun)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\release_common.ps1"
$cfg = Get-ReleaseConfig
$flag = if ($DryRun) { ' --dry-run' } else { '' }
Write-Gate "Server cleanup on $($cfg.target)$flag"
& ssh -o ConnectTimeout=20 -o BatchMode=yes $cfg.target "bash $($cfg.remoteDir)/deploy/server_cleanup.sh$flag; echo; echo 'Last runs:'; tail -n 5 /var/log/erp-cleanup.log; echo; df -h / | tail -n 1"
if ($LASTEXITCODE -ne 0) { Write-Host 'Cleanup failed - see above.' -ForegroundColor Red; exit 1 }
