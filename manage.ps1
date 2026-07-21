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
  .\manage.ps1 backup               create timestamped, AES-256 encrypted custom-format dumps of
                                     dev, test, and live (see docs/operations/backup_restore.md)
  .\manage.ps1 restore -Env dev -File .\backups\dev\custom_erp_....dump.enc
                                     restore one environment after an explicit confirmation; its ERP
                                     server must be stopped first. Also accepts legacy unencrypted
                                     .dump files from before encryption was added.
  .\manage.ps1 restore-drill        automated monthly recovery drill: restores the newest dev backup
                                     into 'test', verifies row counts, logs the result. Never targets live.
  .\manage.ps1 register-schedule    registers Windows Scheduled Tasks for daily backup (02:00) and
                                     monthly restore-drill (1st, 03:00). Run once per machine.
  .\manage.ps1 fleet-status         one-shot report across all 3 environments: port up/down, live
                                     GET /api/v1/version (commit/build time), last recorded promotion.

Postgres itself (portable install, port 5435) is shared across all 3 environments - only the
database differs per environment (see environments.json) - so 'start'/'stop' -Env test|live only
start/stop that environment's erp-server.exe, never a second Postgres instance.
#>

param(
    [Parameter(Position = 0)]
    [ValidateSet("start", "stop", "restart", "status", "logs", "release", "backup", "restore", "restore-drill", "register-schedule", "fleet-status")]
    [string]$Action,

    [ValidateSet("dev", "test", "live")]
    [string]$Env = "dev",

    [string]$File,

    # Enables an attended or automated restore only when it exactly matches
    # "RESTORE <environment>". Omit it to be prompted interactively.
    [string]$ConfirmRestore
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
$BackupRoot = Join-Path $RepoRoot "backups"

if (Test-Path $ErpDir) {
    if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir | Out-Null }
} elseif ($Env -ne "dev" -and $Action -notin @("backup", "restore")) {
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
        & "$PgBin\pg_isready.exe" -p $PgPort 2>$null | Out-Null
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
        Write-Host "erp-server.exe not found. Build it first:  go build -o erp-server.exe ./cmd/server" -ForegroundColor Red
        return
    }
    if (-not (Test-PortOpen $PgPort)) {
        Write-Host "Postgres isn't running yet - the server would crash on startup. Starting Postgres first..." -ForegroundColor Yellow
        Start-Pg
    }
    Write-Host "Starting ERP server ('$Env', port $ErpPort, database '$ErpDatabase')..." -ForegroundColor Cyan
    # PORT/DATABASE_URL (Stage 14.9) let dev/test/live run the same binary
    # side by side - for 'dev' these match internal/server/routes.go's own hardcoded defaults
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
    # (internal/server/server.go's defaults, meant for a bare `go build` during iterative dev).
    $commitHash = (git rev-parse --short HEAD 2>$null)
    if (-not $commitHash) { $commitHash = "unknown" }
    $buildTimestamp = (Get-Date -AsUTC -Format "yyyy-MM-ddTHH:mm:ssZ")
    $ldflags = "-s -w -X custom_erp/internal/server.gitCommit=$commitHash -X custom_erp/internal/server.buildTime=$buildTimestamp"

    Write-Host "Building stripped release binary (-ldflags=`"$ldflags`")..." -ForegroundColor Cyan
    Push-Location $ErpDir
    try {
        & "$GoBin\go.exe" build -ldflags="$ldflags" -o erp-server.exe ./cmd/server
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

# Send-OpsAlert posts a short message to the same Slack/Teams-compatible
# incoming webhook the Go server uses (OPS_ALERT_WEBHOOK_URL, see
# engines/alerting.go) - kept as a plain env var, not a script param, so the
# same one value configures both halves of Stage 17.10's alerting. No-ops
# quietly if the env var isn't set, same as the Go side.
function Send-OpsAlert($Message) {
    $webhookUrl = $env:OPS_ALERT_WEBHOOK_URL
    if (-not $webhookUrl) { return }
    try {
        $body = @{ text = ":rotating_light: [manage.ps1] $Message" } | ConvertTo-Json
        Invoke-RestMethod -Uri $webhookUrl -Method Post -ContentType "application/json" -Body $body -TimeoutSec 5 | Out-Null
    } catch {
        Write-Host "  (ops alert delivery failed: $($_.Exception.Message))" -ForegroundColor DarkYellow
    }
}

# Stage 12.2: backups-at-rest encryption via .NET's built-in AES (System.Security.Cryptography,
# no new dependency - same "stdlib only" approach as engines/mfa.go's TOTP). Key resolution mirrors
# the JWT_SECRET pattern already established in engines/auth.go: BACKUP_ENCRYPTION_KEY env var if
# set, otherwise a 32-byte key is generated once and persisted outside the repo (never git-tracked)
# so encryption works out of the box with zero required setup.
function Get-BackupEncryptionKey {
    if ($env:BACKUP_ENCRYPTION_KEY) {
        $raw = [System.Text.Encoding]::UTF8.GetBytes($env:BACKUP_ENCRYPTION_KEY)
        $sha256 = [System.Security.Cryptography.SHA256]::Create()
        return $sha256.ComputeHash($raw)
    }
    $keyPath = Join-Path $env:USERPROFILE ".erp-backup-key"
    if (-not (Test-Path $keyPath)) {
        $bytes = New-Object byte[] 32
        [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
        [Convert]::ToBase64String($bytes) | Set-Content -LiteralPath $keyPath -NoNewline
        Write-Host "Generated a new backup encryption key at $keyPath - back this up separately from the repo; losing it makes existing encrypted backups unrecoverable." -ForegroundColor Yellow
    }
    return [Convert]::FromBase64String((Get-Content -LiteralPath $keyPath -Raw))
}

# Encrypts $PlainPath in place, writing "$PlainPath.enc" (IV prefixed to the ciphertext stream) and
# removing the plaintext afterward. Returns the .enc path.
function Protect-BackupFile($PlainPath) {
    $aes = [System.Security.Cryptography.Aes]::Create()
    try {
        $aes.Key = Get-BackupEncryptionKey
        $aes.GenerateIV()
        $encPath = "$PlainPath.enc"
        $fsOut = [System.IO.File]::Create($encPath)
        try {
            $fsOut.Write($aes.IV, 0, $aes.IV.Length)
            $cryptoStream = New-Object System.Security.Cryptography.CryptoStream($fsOut, $aes.CreateEncryptor(), [System.Security.Cryptography.CryptoStreamMode]::Write)
            $fsIn = [System.IO.File]::OpenRead($PlainPath)
            try { $fsIn.CopyTo($cryptoStream) } finally { $fsIn.Close() }
            $cryptoStream.FlushFinalBlock()
            $cryptoStream.Close()
        } finally {
            $fsOut.Close()
        }
        Remove-Item -LiteralPath $PlainPath -Force
        return $encPath
    } finally {
        $aes.Dispose()
    }
}

# Decrypts $EncPath to a fresh temp file and returns its path; caller is responsible for deleting it.
function Unprotect-BackupFile($EncPath) {
    $aes = [System.Security.Cryptography.Aes]::Create()
    try {
        $aes.Key = Get-BackupEncryptionKey
        $outPath = Join-Path ([System.IO.Path]::GetTempPath()) ("erp_restore_" + [guid]::NewGuid().ToString("N") + ".dump")
        $fsIn = [System.IO.File]::OpenRead($EncPath)
        try {
            $iv = New-Object byte[] 16
            $null = $fsIn.Read($iv, 0, 16)
            $aes.IV = $iv
            $cryptoStream = New-Object System.Security.Cryptography.CryptoStream($fsIn, $aes.CreateDecryptor(), [System.Security.Cryptography.CryptoStreamMode]::Read)
            $fsOut = [System.IO.File]::Create($outPath)
            try { $cryptoStream.CopyTo($fsOut) } finally { $fsOut.Close() }
        } finally {
            $fsIn.Close()
        }
        return $outPath
    } finally {
        $aes.Dispose()
    }
}

function Backup-Databases {
    # Stage 17.10: any failure below alerts to OPS_ALERT_WEBHOOK_URL (via
    # Send-OpsAlert) before propagating, so a failed scheduled backup (see
    # docs/operations/backup_restore.md's Task Scheduler recipe) is caught the same day
    # rather than silently discovered at the next restore drill.
    try {
        if (-not (Test-PortOpen $PgPort)) { throw "PostgreSQL is not running on port $PgPort; start it before backing up." }
        if (-not (Test-Path "$PgBin\pg_dump.exe")) { throw "pg_dump.exe not found at $PgBin." }
        $timestamp = Get-Date -AsUTC -Format "yyyyMMddTHHmmssZ"
        $backupCount = 0
        foreach ($name in @("dev", "test", "live")) {
            $cfg = Resolve-Env $name
            $exists = & "$PgBin\psql.exe" -h localhost -p $cfg.PgPort -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$($cfg.Database)'" 2>$null
            if (-not $exists -or $exists.Trim() -ne "1") {
                Write-Host "Skipping ${name}: database '$($cfg.Database)' does not exist yet." -ForegroundColor Yellow
                continue
            }
            $targetDir = Join-Path $BackupRoot $name
            if (-not (Test-Path $targetDir)) { New-Item -ItemType Directory -Path $targetDir -Force | Out-Null }
            $target = Join-Path $targetDir ("{0}_{1}.dump" -f $cfg.Database, $timestamp)
            Write-Host "Backing up $name database '$($cfg.Database)'..." -ForegroundColor Cyan
            & "$PgBin\pg_dump.exe" -h localhost -p $cfg.PgPort -U postgres -F c "--file=$target" $cfg.Database
            if ($LASTEXITCODE -ne 0) { throw "Backup failed for $name database '$($cfg.Database)'." }
            $encTarget = Protect-BackupFile $target
            $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $encTarget).Hash
            Set-Content -LiteralPath "$encTarget.sha256" -Value "$hash  $(Split-Path $encTarget -Leaf)"
            Write-Host "  Saved $encTarget (AES-256 encrypted)" -ForegroundColor Green
            $backupCount++
        }
        if ($backupCount -eq 0) { throw "No configured ERP databases exist to back up." }
    } catch {
        Send-OpsAlert "Database backup FAILED: $($_.Exception.Message)"
        throw
    }
}

function Restore-Database {
    if (-not $File) { throw "restore requires -File <backup.dump.enc | backup.dump>." }
    $backupFile = Resolve-Path -LiteralPath $File -ErrorAction Stop
    if (-not (Test-PortOpen $PgPort)) { throw "PostgreSQL is not running on port $PgPort." }
    if (Test-PortOpen $ErpPort) { throw "ERP server for '$Env' is running on port $ErpPort. Stop it before restoring '$ErpDatabase'." }
    if (-not (Test-Path "$PgBin\pg_restore.exe")) { throw "pg_restore.exe not found at $PgBin." }
    Write-Host "WARNING: this will replace the contents of '$ErpDatabase' from '$backupFile'." -ForegroundColor Yellow
    $confirmation = if ($ConfirmRestore) { $ConfirmRestore } else { Read-Host "Type RESTORE $Env to continue" }
    if ($confirmation -cne "RESTORE $Env") { Write-Host "Restore cancelled." -ForegroundColor Yellow; return }

    # Backups produced since encryption was added end in .dump.enc; older plaintext .dump files
    # from before this change still restore directly, no migration needed.
    $tempPlain = $null
    $restoreSource = $backupFile.Path
    if ($restoreSource -like "*.enc") {
        $tempPlain = Unprotect-BackupFile $restoreSource
        $restoreSource = $tempPlain
    }
    try {
        & "$PgBin\pg_restore.exe" -h localhost -p $PgPort -U postgres -d $ErpDatabase --clean --if-exists --no-owner $restoreSource
        if ($LASTEXITCODE -ne 0) { throw "Restore failed for '$ErpDatabase'. Review PostgreSQL output before starting ERP." }
    } finally {
        if ($tempPlain -and (Test-Path $tempPlain)) { Remove-Item -LiteralPath $tempPlain -Force }
    }
    Write-Host "Restore complete for '$Env' database '$ErpDatabase'." -ForegroundColor Green
}

# Stage 12.2: makes "monthly recovery test drill" a real, scriptable, non-interactive action instead
# of a manual runbook step someone has to remember - always sources the newest dev backup and always
# restores into 'test', matching docs/operations/backup_restore.md's own established drill pattern.
# Deliberately refuses to ever target 'live'.
function Invoke-RestoreDrill {
    if ($Env -eq "live") { throw "restore-drill refuses to target 'live' - it always restores into 'test' regardless of -Env." }
    $started = Get-Date
    try {
        $devBackupDir = Join-Path $BackupRoot "dev"
        $latest = Get-ChildItem -LiteralPath $devBackupDir -Filter "*.dump*" -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -notlike "*.sha256" } |
            Sort-Object LastWriteTime -Descending | Select-Object -First 1
        if (-not $latest) { throw "No dev backup found under $devBackupDir - run '.\manage.ps1 backup' first." }

        $testCfg = Resolve-Env "test"
        $testDbExists = (& "$PgBin\psql.exe" -h localhost -p $testCfg.PgPort -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$($testCfg.Database)'" 2>$null)
        if (-not $testDbExists -or $testDbExists.Trim() -ne "1") { throw "Database '$($testCfg.Database)' does not exist yet - provision it (e.g. via promote.ps1 -To test) before the first drill." }
        if (Test-PortOpen $testCfg.ErpPort) {
            Write-Host "Stopping 'test' ERP server (port $($testCfg.ErpPort)) for the drill..." -ForegroundColor Yellow
            $conns = Get-NetTCPConnection -LocalPort $testCfg.ErpPort -State Listen -ErrorAction SilentlyContinue
            $conns | ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue }
        }

        Write-Host "Restoring $($latest.Name) into 'test' database '$($testCfg.Database)'..." -ForegroundColor Cyan
        $tempPlain = $null
        $restoreSource = $latest.FullName
        if ($restoreSource -like "*.enc") {
            $tempPlain = Unprotect-BackupFile $restoreSource
            $restoreSource = $tempPlain
        }
        try {
            & "$PgBin\pg_restore.exe" -h localhost -p $testCfg.PgPort -U postgres -d $testCfg.Database --clean --if-exists --no-owner $restoreSource
            if ($LASTEXITCODE -ne 0) { throw "pg_restore failed against 'test'." }
        } finally {
            if ($tempPlain -and (Test-Path $tempPlain)) { Remove-Item -LiteralPath $tempPlain -Force }
        }

        # Same sanity checks as the manual drill this replaces (docs/operations/backup_restore.md's
        # "Latest verified restore drill" entry, 2026-07-19): row counts on two core tables.
        $tenantCount = (& "$PgBin\psql.exe" -h localhost -p $testCfg.PgPort -U postgres -d $testCfg.Database -tAc "SELECT COUNT(*) FROM public.tenants" 2>$null).Trim()
        $doctypeCount = (& "$PgBin\psql.exe" -h localhost -p $testCfg.PgPort -U postgres -d $testCfg.Database -tAc "SELECT COUNT(*) FROM tenant_default.doctype_meta" 2>$null).Trim()
        if (-not $tenantCount -or -not $doctypeCount) { throw "Restore ran but post-restore verification queries failed." }

        $duration = [math]::Round(((Get-Date) - $started).TotalSeconds, 1)
        $logLine = "- $(Get-Date -AsUTC -Format 'yyyy-MM-ddTHH:mm:ssZ') | backup=$($latest.Name) | target=test/$($testCfg.Database) | duration=${duration}s | tenants=$tenantCount doctype_meta=$doctypeCount | verifier=automated (manage.ps1 restore-drill) | result=PASS"
        Add-Content -LiteralPath (Join-Path $RepoRoot "docs\operations\restore_drill_log.md") -Value $logLine
        Write-Host "Restore drill PASSED in ${duration}s (tenants=$tenantCount, doctype_meta=$doctypeCount). Logged to docs/operations/restore_drill_log.md." -ForegroundColor Green
    } catch {
        $duration = [math]::Round(((Get-Date) - $started).TotalSeconds, 1)
        $logLine = "- $(Get-Date -AsUTC -Format 'yyyy-MM-ddTHH:mm:ssZ') | duration=${duration}s | verifier=automated (manage.ps1 restore-drill) | result=FAIL | error=$($_.Exception.Message)"
        Add-Content -LiteralPath (Join-Path $RepoRoot "docs\operations\restore_drill_log.md") -Value $logLine
        Send-OpsAlert "Monthly restore drill FAILED: $($_.Exception.Message)"
        throw
    }
}

# Stage 12.2: converts docs/operations/backup_restore.md's manual Task Scheduler recipe into one
# command. Registers two tasks (idempotent - /F overwrites a prior registration of the same name)
# rather than silently running on its own; this only executes when explicitly invoked.
function Register-BackupSchedule {
    $manageScript = Join-Path $RepoRoot "manage.ps1"
    $backupCmd = "-NoProfile -ExecutionPolicy Bypass -File `"$manageScript`" backup"
    $drillCmd = "-NoProfile -ExecutionPolicy Bypass -File `"$manageScript`" restore-drill"

    schtasks.exe /Create /TN "ERP-DailyBackup" /TR "powershell.exe $backupCmd" /SC DAILY /ST 02:00 /RL LIMITED /F | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Failed to register ERP-DailyBackup scheduled task." }
    schtasks.exe /Create /TN "ERP-MonthlyRestoreDrill" /TR "powershell.exe $drillCmd" /SC MONTHLY /D 1 /ST 03:00 /RL LIMITED /F | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Failed to register ERP-MonthlyRestoreDrill scheduled task." }

    Write-Host "Registered scheduled tasks:" -ForegroundColor Green
    Write-Host "  ERP-DailyBackup          daily 02:00  -> manage.ps1 backup" -ForegroundColor Cyan
    Write-Host "  ERP-MonthlyRestoreDrill  1st @ 03:00  -> manage.ps1 restore-drill" -ForegroundColor Cyan
    Write-Host "Manage them via taskschd.msc or 'schtasks /Query /TN ERP-DailyBackup'." -ForegroundColor DarkGray
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
        "backup" { Backup-Databases }
        "restore" { Restore-Database }
        "restore-drill" { Invoke-RestoreDrill }
        "register-schedule" { Register-BackupSchedule }
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
    Write-Host "  7) Backup dev/test/live databases (encrypted)"
    Write-Host "  8) Fleet status (dev/test/live)"
    Write-Host "  9) Run restore drill (dev backup -> test)"
    Write-Host "  0) Exit"
    $choice = Read-Host "`nChoose an option"
    switch ($choice) {
        "1" { Invoke-Action "start" }
        "2" { Invoke-Action "stop" }
        "3" { Invoke-Action "restart" }
        "4" { Invoke-Action "status" }
        "5" { Invoke-Action "logs" }
        "6" { Invoke-Action "release" }
        "7" { Invoke-Action "backup" }
        "8" { Invoke-Action "fleet-status" }
        "9" { Invoke-Action "restore-drill" }
        "0" { exit }
        default { Write-Host "Invalid choice." -ForegroundColor Red }
    }
}
