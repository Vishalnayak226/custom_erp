<#
Start/stop/restart the ERP dev stack (PostgreSQL + Go server) from one place.

Usage:
  .\manage.ps1              interactive menu
  .\manage.ps1 start        start Postgres, wait until ready, then start erp-server
  .\manage.ps1 stop         stop erp-server, then stop Postgres
  .\manage.ps1 restart      stop then start
  .\manage.ps1 status       show what's currently running
  .\manage.ps1 logs         show the last lines of both log files
  .\manage.ps1 release      stop erp-server if running, rebuild it stripped (-ldflags="-s -w"),
                             report the size change. Does not restart it - run 'start' after.
#>

param(
    [Parameter(Position = 0)]
    [ValidateSet("start", "stop", "restart", "status", "logs", "release")]
    [string]$Action
)

$ErrorActionPreference = "Stop"

$PgBin    = "$env:USERPROFILE\pg-portable\pgsql\bin"
$PgData   = "$env:USERPROFILE\pg-data"
$PgPort   = 5435
$GoBin    = "$env:USERPROFILE\go-portable\go\bin"
$ErpDir   = $PSScriptRoot
$ErpExe   = Join-Path $ErpDir "erp-server.exe"
$ErpPort  = 8080
$LogDir   = Join-Path $ErpDir "logs"
$PgLog    = Join-Path $LogDir "postgres.log"
$ErpOutLog = Join-Path $LogDir "erp-server.out.log"
$ErpErrLog = Join-Path $LogDir "erp-server.err.log"

if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir | Out-Null }

function Test-PortOpen($port) {
    return [bool](Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
}

function Show-Status {
    $pgUp  = Test-PortOpen $PgPort
    $erpUp = Test-PortOpen $ErpPort
    Write-Host ""
    Write-Host "  PostgreSQL  (port $PgPort)  " -NoNewline
    if ($pgUp)  { Write-Host "RUNNING" -ForegroundColor Green } else { Write-Host "STOPPED" -ForegroundColor Red }
    Write-Host "  ERP Server  (port $ErpPort)  " -NoNewline
    if ($erpUp) { Write-Host "RUNNING" -ForegroundColor Green } else { Write-Host "STOPPED" -ForegroundColor Red }
    Write-Host ""
    if ($erpUp) { Write-Host "  -> http://localhost:$ErpPort" -ForegroundColor Cyan; Write-Host "" }
}

function Start-Pg {
    if (Test-PortOpen $PgPort) {
        Write-Host "Postgres already running on $PgPort." -ForegroundColor Yellow
        return
    }
    if (-not (Test-Path "$PgBin\pg_ctl.exe")) {
        Write-Host "pg_ctl.exe not found at $PgBin - check the portable Postgres install path." -ForegroundColor Red
        return
    }
    Write-Host "Starting Postgres..." -ForegroundColor Cyan
    & "$PgBin\pg_ctl.exe" start -D "$PgData" -l "$PgLog" -o "-p $PgPort" -w | Out-Null

    for ($i = 0; $i -lt 15; $i++) {
        $ready = & "$PgBin\pg_isready.exe" -p $PgPort 2>$null
        if ($LASTEXITCODE -eq 0) { Write-Host "Postgres is ready." -ForegroundColor Green; return }
        Start-Sleep -Seconds 1
    }
    Write-Host "Postgres did not report ready within 15s - check $PgLog" -ForegroundColor Red
}

function Stop-Pg {
    if (-not (Test-PortOpen $PgPort)) {
        Write-Host "Postgres is not running." -ForegroundColor Yellow
        return
    }
    Write-Host "Stopping Postgres..." -ForegroundColor Cyan
    & "$PgBin\pg_ctl.exe" stop -D "$PgData" -m fast -w | Out-Null
    Write-Host "Postgres stopped." -ForegroundColor Green
}

function Start-Erp {
    if (Test-PortOpen $ErpPort) {
        Write-Host "ERP server already running on $ErpPort." -ForegroundColor Yellow
        return
    }
    if (-not (Test-Path $ErpExe)) {
        Write-Host "erp-server.exe not found. Build it first:  go build -o erp-server.exe" -ForegroundColor Red
        return
    }
    if (-not (Test-PortOpen $PgPort)) {
        Write-Host "Postgres isn't running yet - the server would crash on startup. Starting Postgres first..." -ForegroundColor Yellow
        Start-Pg
    }
    Write-Host "Starting ERP server..." -ForegroundColor Cyan
    Start-Process -FilePath $ErpExe -WorkingDirectory $ErpDir -WindowStyle Hidden `
        -RedirectStandardOutput $ErpOutLog -RedirectStandardError $ErpErrLog

    for ($i = 0; $i -lt 10; $i++) {
        if (Test-PortOpen $ErpPort) { Write-Host "ERP server is up: http://localhost:$ErpPort" -ForegroundColor Green; return }
        Start-Sleep -Milliseconds 500
    }
    Write-Host "Server didn't come up within 5s - check $ErpErrLog" -ForegroundColor Red
}

function Stop-Erp {
    $proc = Get-Process -Name "erp-server" -ErrorAction SilentlyContinue
    if (-not $proc) {
        Write-Host "ERP server is not running." -ForegroundColor Yellow
        return
    }
    Write-Host "Stopping ERP server..." -ForegroundColor Cyan
    $proc | Stop-Process -Force
    Write-Host "ERP server stopped." -ForegroundColor Green
}

function Build-Release {
    if (-not (Test-Path "$GoBin\go.exe")) {
        Write-Host "go.exe not found at $GoBin - check the portable Go install path." -ForegroundColor Red
        return
    }

    # Windows locks a running .exe - stop the server first if it's up, since go build would
    # otherwise fail trying to overwrite it. Deliberately does not restart afterward - run
    # 'start' yourself when ready, matching how every other change in this project gets applied.
    $wasRunning = Test-PortOpen $ErpPort
    if ($wasRunning) {
        Write-Host "Stopping erp-server.exe first (can't overwrite a running binary on Windows)..." -ForegroundColor Yellow
        Stop-Erp
    }

    $beforeSize = $null
    if (Test-Path $ErpExe) { $beforeSize = (Get-Item $ErpExe).Length }

    Write-Host "Building stripped release binary (-ldflags=`"-s -w`")..." -ForegroundColor Cyan
    Push-Location $ErpDir
    try {
        & "$GoBin\go.exe" build -ldflags="-s -w" -o erp-server.exe
    } finally {
        Pop-Location
    }

    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build failed." -ForegroundColor Red
        return
    }

    $afterSize = (Get-Item $ErpExe).Length
    Write-Host "Build OK: erp-server.exe" -ForegroundColor Green
    if ($beforeSize) {
        $savedKB = [math]::Round(($beforeSize - $afterSize) / 1KB, 0)
        Write-Host ("  {0:N0} KB -> {1:N0} KB  ({2:N0} KB smaller)" -f ($beforeSize / 1KB), ($afterSize / 1KB), $savedKB) -ForegroundColor Cyan
    } else {
        Write-Host ("  {0:N0} KB" -f ($afterSize / 1KB)) -ForegroundColor Cyan
    }
    if ($wasRunning) {
        Write-Host "Server was stopped to rebuild it - run '.\manage.ps1 start' when you're ready." -ForegroundColor Yellow
    }
}

function Show-Logs {
    Write-Host "`n--- erp-server.out.log (last 20 lines) ---" -ForegroundColor Cyan
    if (Test-Path $ErpOutLog) { Get-Content $ErpOutLog -Tail 20 } else { Write-Host "(no log yet)" }
    Write-Host "`n--- erp-server.err.log (last 20 lines) ---" -ForegroundColor Cyan
    if (Test-Path $ErpErrLog) { Get-Content $ErpErrLog -Tail 20 } else { Write-Host "(no log yet)" }
    Write-Host "`n--- postgres.log (last 20 lines) ---" -ForegroundColor Cyan
    if (Test-Path $PgLog) { Get-Content $PgLog -Tail 20 } else { Write-Host "(no log yet)" }
    Write-Host ""
}

function Invoke-Action($a) {
    switch ($a) {
        "start"   { Start-Pg; Start-Erp; Show-Status }
        "stop"    { Stop-Erp; Stop-Pg; Show-Status }
        "restart" { Stop-Erp; Stop-Pg; Start-Sleep -Seconds 1; Start-Pg; Start-Erp; Show-Status }
        "status"  { Show-Status }
        "logs"    { Show-Logs }
        "release" { Build-Release }
    }
}

if ($Action) {
    Invoke-Action $Action
    exit
}

# No argument given -> interactive menu
while ($true) {
    Write-Host "`n==== Custom ERP Dev Stack ====" -ForegroundColor Magenta
    Show-Status
    Write-Host "  1) Start"
    Write-Host "  2) Stop"
    Write-Host "  3) Restart"
    Write-Host "  4) Status"
    Write-Host "  5) Show logs"
    Write-Host "  6) Build stripped release binary"
    Write-Host "  0) Exit"
    $choice = Read-Host "`nChoose an option"
    switch ($choice) {
        "1" { Invoke-Action "start" }
        "2" { Invoke-Action "stop" }
        "3" { Invoke-Action "restart" }
        "4" { Invoke-Action "status" }
        "5" { Invoke-Action "logs" }
        "6" { Invoke-Action "release" }
        "0" { exit }
        default { Write-Host "Invalid choice." -ForegroundColor Red }
    }
}
