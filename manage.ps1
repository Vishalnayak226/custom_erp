<#
Start/stop/restart the ERP dev/test/live stack (PostgreSQL + Go server) from one place.

Usage:
  .\manage.ps1                     interactive menu (dev)
  .\manage.ps1 start                start Postgres, wait until ready, then start erp-server (dev)
  .\manage.ps1 stop                 stop erp-server, then stop Postgres (dev)
  .\manage.ps1 restart              stop then start (dev)
  .\manage.ps1 status               show what's currently running (dev)
  .\manage.ps1 logs                 show the last lines of both log files (dev)
  .\manage.ps1 release              stop erp-server if running, rebuild it stripped (-ldflags="-s -w"),
                                     report the size change. Does not restart it - run 'start' after.
  .\manage.ps1 <action> -Env test   same actions, targeting the 'test' environment instead of 'dev'
                                     (own port/database, per environments.json - see promote.ps1
                                     for how a commit actually gets there). -Env live works the same way.
  .\manage.ps1 fleet-status         one-shot report across all 3 environments: port up/down, live
                                     GET /api/v1/version (commit/build time), last recorded promotion.

Postgres itself (portable install, port 5435) is shared across all 3 environments - only the
database differs per environment (see environments.json) - so 'start'/'stop' -Env test|live only
start/stop that environment's erp-server.exe, never a second Postgres instance.
#>

param(
    [Parameter(Position = 0)]
    [ValidateSet("start", "stop", "restart", "status", "logs", "release", "fleet-status")]
    [string]$Action,

    [ValidateSet("dev", "test", "live")]
    [string]$Env = "dev"
)

$ErrorActionPreference = "Stop"

$RepoRoot = $PSScriptRoot
$envConfigPath = Join-Path $RepoRoot "environments.json"
$envConfig = if (Test-Path $envConfigPath) { Get-Content $envConfigPath -Raw | ConvertFrom-Json } else { $null }

# Stage 14.9: resolve which directory/port/database this invocation targets.
# 'dev' always resolves to this exact working tree and the original
# hardcoded 5435/8080/custom_erp - byte-for-byte the same as before
# environments.json existed, so a bare `.\manage.ps1 start` behaves exactly
# as it always has. 'test'/'live' resolve to their own git worktree
# (created by promote.ps1, not this script) and their own database.
function Resolve-Env($envName) {
    if ($envName -eq "dev" -or -not $envConfig) {
        return @{ ErpDir = $RepoRoot; PgPort = 5435; ErpPort = 8080; Database = "custom_erp" }
    }
    $cfg = $envConfig.$envName
    $worktreePath = if ($cfg.worktree) { Join-Path $env:USERPROFILE $cfg.worktree } else { $RepoRoot }
    return @{ ErpDir = $worktreePath; PgPort = $cfg.pgPort; ErpPort = $cfg.erpPort; Database = $cfg.database }
}

$resolved = Resolve-Env $Env
$PgBin    = "$env:USERPROFILE\pg-portable\pgsql\bin"
$PgData   = "$env:USERPROFILE\pg-data"
$PgPort   = $resolved.PgPort
$GoBin    = "$env:USERPROFILE\go-portable\go\bin"
$ErpDir   = $resolved.ErpDir
$ErpExe   = Join-Path $ErpDir "erp-server.exe"
$ErpPort  = $resolved.ErpPort
$ErpDatabase = $resolved.Database
$LogDir   = Join-Path $ErpDir "logs"
$PgLog    = Join-Path $LogDir "postgres.log"
$ErpOutLog = Join-Path $LogDir "erp-server.out.log"
$ErpErrLog = Join-Path $LogDir "erp-server.err.log"

if (Test-Path $ErpDir) {
    if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir | Out-Null }
} elseif ($Env -ne "dev") {
    Write-Host "'$Env' has no worktree yet at $ErpDir - run promote.ps1 -To $Env at least once first." -ForegroundColor Red
    exit 1
}

function Test-PortOpen($port) {
    return [bool](Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
}

function Show-Status {
    $pgUp  = Test-PortOpen $PgPort
    $erpUp = Test-PortOpen $ErpPort
    Write-Host ""
    Write-Host "  Environment: $Env  (database: $ErpDatabase)" -ForegroundColor DarkGray
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
    Write-Host "Starting ERP server ('$Env', port $ErpPort, database '$ErpDatabase')..." -ForegroundColor Cyan
    # PORT/DATABASE_URL (Stage 14.9) let dev/test/live run the same binary
    # side by side - for 'dev' these match main.go's own hardcoded defaults
    # exactly, so setting them here changes nothing about dev's behavior.
    $env:PORT = "$ErpPort"
    $env:DATABASE_URL = "postgres://postgres@localhost:$PgPort/$ErpDatabase`?sslmode=disable"
    Start-Process -FilePath $ErpExe -WorkingDirectory $ErpDir -WindowStyle Hidden `
        -RedirectStandardOutput $ErpOutLog -RedirectStandardError $ErpErrLog
    Remove-Item Env:\PORT, Env:\DATABASE_URL -ErrorAction SilentlyContinue

    for ($i = 0; $i -lt 10; $i++) {
        if (Test-PortOpen $ErpPort) { Write-Host "ERP server is up: http://localhost:$ErpPort" -ForegroundColor Green; return }
        Start-Sleep -Milliseconds 500
    }
    Write-Host "Server didn't come up within 5s - check $ErpErrLog" -ForegroundColor Red
}

function Stop-Erp {
    # Targeted by port, not by process name - dev/test/live can all be
    # running erp-server.exe simultaneously (Stage 14.9), so "the process
    # named erp-server" is ambiguous once more than one environment is up.
    $conns = Get-NetTCPConnection -LocalPort $ErpPort -State Listen -ErrorAction SilentlyContinue
    if (-not $conns) {
        Write-Host "ERP server ('$Env') is not running." -ForegroundColor Yellow
        return
    }
    Write-Host "Stopping ERP server ('$Env', port $ErpPort)..." -ForegroundColor Cyan
    $conns | ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue }
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

    # Stage 14.6: stamp the binary with its real git commit and build time so
    # GET /api/v1/version reports something more useful than "dev"/"unknown"
    # (main.go's defaults, meant for a bare `go build` during iterative dev).
    $commitHash = (git rev-parse --short HEAD 2>$null)
    if (-not $commitHash) { $commitHash = "unknown" }
    $buildTimestamp = (Get-Date -AsUTC -Format "yyyy-MM-ddTHH:mm:ssZ")
    $ldflags = "-s -w -X main.gitCommit=$commitHash -X main.buildTime=$buildTimestamp"

    Write-Host "Building stripped release binary (-ldflags=`"$ldflags`")..." -ForegroundColor Cyan
    Push-Location $ErpDir
    try {
        & "$GoBin\go.exe" build -ldflags="$ldflags" -o erp-server.exe
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

function Show-FleetStatus {
    if (-not $envConfig) {
        Write-Host "environments.json not found." -ForegroundColor Red
        return
    }
    Write-Host "`n==== Fleet Status ====" -ForegroundColor Magenta
    foreach ($name in @("dev", "test", "live")) {
        $cfg = Resolve-Env $name
        $up = Test-PortOpen $cfg.ErpPort
        Write-Host "`n  $name  (port $($cfg.ErpPort), database '$($cfg.Database)')" -ForegroundColor Cyan
        if ($up) {
            try {
                $v = Invoke-RestMethod -Uri "http://localhost:$($cfg.ErpPort)/api/v1/version" -TimeoutSec 3
                Write-Host "    RUNNING - version $($v.version), commit $($v.git_commit), built $($v.build_time)" -ForegroundColor Green
            } catch {
                Write-Host "    RUNNING (port open) but /api/v1/version didn't respond: $($_.Exception.Message)" -ForegroundColor Yellow
            }
        } else {
            Write-Host "    STOPPED" -ForegroundColor Red
        }
        # Deployment history is worth showing even when stopped - that's the point of an audit trail.
        try {
            $last = & "$PgBin\psql.exe" -h localhost -p 5435 -U postgres -d custom_erp -tAc `
                "SELECT git_commit || ' (' || build_status || ') at ' || promoted_at FROM public.deployments WHERE environment = '$name' ORDER BY promoted_at DESC LIMIT 1" 2>$null
            if ($last -and $last.Trim()) { Write-Host "    Last promotion: $($last.Trim())" -ForegroundColor DarkGray }
        } catch {}
    }
    Write-Host ""
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
        "fleet-status" { Show-FleetStatus }
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
    Write-Host "  7) Fleet status (dev/test/live)"
    Write-Host "  0) Exit"
    $choice = Read-Host "`nChoose an option"
    switch ($choice) {
        "1" { Invoke-Action "start" }
        "2" { Invoke-Action "stop" }
        "3" { Invoke-Action "restart" }
        "4" { Invoke-Action "status" }
        "5" { Invoke-Action "logs" }
        "6" { Invoke-Action "release" }
        "7" { Invoke-Action "fleet-status" }
        "0" { exit }
        default { Write-Host "Invalid choice." -ForegroundColor Red }
    }
}
