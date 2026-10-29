<#
Cleanup click (Stage 109.3): runs the server cleanup now instead of waiting
for the 03:30 cron. Same script, same rules (deploy/server_cleanup.sh).
-Deep (Stage 109.12) adds the deep pass the 04:15 cron runs every 10 days,
and restarts its 10-day count.

  release\Cleanup.cmd                                       # clean now
  release\Cleanup.cmd -Deep                                 # clean now, deep pass too
  pwsh release\scripts\Invoke-Cleanup.ps1 -DryRun [-Deep]  # say what it would remove
#>
param([switch]$DryRun, [switch]$Deep)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\ReleaseCommon.ps1"
$cfg = Get-ReleaseConfig
$flag = "$(if ($Deep) { ' --deep' })$(if ($DryRun) { ' --dry-run' })"
Write-Gate "Server cleanup on $($cfg.target)$flag"
& ssh -o ConnectTimeout=20 -o BatchMode=yes $cfg.target "bash $($cfg.remoteDir)/deploy/server_cleanup.sh$flag; echo; echo 'Last runs:'; tail -n 5 /var/log/erp-cleanup.log; echo; df -h / | tail -n 1"
if ($LASTEXITCODE -ne 0) { Write-Host 'Cleanup failed - see above.' -ForegroundColor Red; exit 1 }
