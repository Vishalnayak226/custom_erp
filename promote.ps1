<#
Stage 14.9-14.12: promote a commit from one environment to the next
(dev -> test -> live), or push it universally to every live-* environment.

This machine's "control plane" is the dev database (custom_erp on the shared
portable Postgres, localhost:5435) - public.deployments and
public.schema_migrations live there regardless of which environment is being
promoted to, since dev's Postgres is the one instance that's always up. Each
environment otherwise has its own fully separate database
(custom_erp/custom_erp_test/custom_erp_live - see environments.json) for real
data isolation, without needing 3 separate Postgres clusters on one dev box.

Usage:
  .\promote.ps1 -From dev -To test                  promote dev's current HEAD to test
  .\promote.ps1 -From test -To live                  promote test's current HEAD to live
  .\promote.ps1 -To live -TenantScope acme-corp      per-client patch - notes it in the deployments row (the actual
                                                       per-tenant *config* toggle still happens via the module/feature-flag
                                                       APIs, not this script - see the note in migrations_stage14c_pipeline.sql)
  .\promote.ps1 -Rollback -Env test                  re-checkout and restart test's previous recorded commit
#>

param(
    [ValidateSet("dev", "test", "live")]
    [string]$From,

    [ValidateSet("test", "live")]
    [string]$To,

    [string]$TenantScope = "ALL",

    [switch]$Rollback,

    [ValidateSet("test", "live")]
    [string]$Env
)

$ErrorActionPreference = "Stop"
$RepoRoot = $PSScriptRoot
$PgBin = "$env:USERPROFILE\pg-portable\pgsql\bin"
$GoBin = "$env:USERPROFILE\go-portable\go\bin"
$PgPort = 5435
$ControlPlaneDb = "custom_erp"

$envConfig = Get-Content (Join-Path $RepoRoot "environments.json") -Raw | ConvertFrom-Json

function Get-WorktreePath($envName) {
    $cfg = $envConfig.$envName
    if ($null -eq $cfg.worktree -or $cfg.worktree -eq "") { return $RepoRoot }
    return Join-Path $env:USERPROFILE $cfg.worktree
}

function Invoke-Psql([string]$Database, [string]$Sql, [string]$File) {
    $psqlArgs = @("-h", "localhost", "-p", $PgPort, "-U", "postgres", "-d", $Database, "-v", "ON_ERROR_STOP=1")
    if ($File) { $psqlArgs += @("-f", $File) } else { $psqlArgs += @("-c", $Sql) }
    & "$PgBin\psql.exe" @psqlArgs
    if ($LASTEXITCODE -ne 0) { throw "psql against '$Database' failed (exit $LASTEXITCODE)" }
}

function Initialize-Database($dbName) {
    $exists = & "$PgBin\psql.exe" -h localhost -p $PgPort -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$dbName'"
    if ($exists -ne "1") {
        Write-Host "Creating database '$dbName'..." -ForegroundColor Cyan
        & "$PgBin\psql.exe" -h localhost -p $PgPort -U postgres -d postgres -c "CREATE DATABASE $dbName"
    }
}

function Invoke-PendingMigrations($dbName, $sourcePath) {
    # Stage 30.2.2: this used to be a second, independent implementation of
    # "apply pending migrations" - a psql loop over db/*.sql in plain
    # lexicographic order, recording each in public.schema_migrations. It now
    # delegates to the runner in the application itself (db/migrate.go), so
    # there is exactly one implementation of the ordering rules, the ledger
    # bookkeeping, and the per-file transaction. `erp-server -migrate` is also
    # what an operator runs by hand against a live database, so promotion and
    # manual maintenance can no longer drift apart.
    Push-Location $sourcePath
    try {
        $env:DATABASE_URL = "postgres://postgres@localhost:$PgPort/$dbName?sslmode=disable"
        & "$GoBin\go.exe" run ./cmd/server -migrate
        if ($LASTEXITCODE -ne 0) { throw "migrations failed against '$dbName'" }
    } finally {
        Remove-Item Env:\DATABASE_URL -ErrorAction SilentlyContinue
        Pop-Location
    }
}

function Add-Deployment($environment, $commit, $appVersion, $status, $notes) {
    $escapedNotes = $notes -replace "'", "''"
    $sql = "INSERT INTO public.deployments (environment, tenant_scope, git_commit, app_version, promoted_by, build_status, notes) VALUES ('$environment', '$TenantScope', '$commit', '$appVersion', '$env:USERNAME', '$status', '$escapedNotes')"
    Invoke-Psql -Database $ControlPlaneDb -Sql $sql
}

function Start-Environment($envName, $worktreePath) {
    $cfg = $envConfig.$envName
    $exe = Join-Path $worktreePath "erp-server.exe"
    if (-not (Test-Path $exe)) { throw "Built binary not found at $exe" }

    $env:PORT = "$($cfg.erpPort)"
    $env:DATABASE_URL = "postgres://postgres@localhost:$($cfg.pgPort)/$($cfg.database)?sslmode=disable"
    $logDir = Join-Path $worktreePath "logs"
    if (-not (Test-Path $logDir)) { New-Item -ItemType Directory -Path $logDir | Out-Null }

    Write-Host "Starting '$envName' on port $($cfg.erpPort) against database '$($cfg.database)'..." -ForegroundColor Cyan
    Start-Process -FilePath $exe -WorkingDirectory $worktreePath -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $logDir "erp-server.out.log") `
        -RedirectStandardError (Join-Path $logDir "erp-server.err.log")

    Remove-Item Env:\PORT, Env:\DATABASE_URL -ErrorAction SilentlyContinue

    for ($i = 0; $i -lt 10; $i++) {
        if ([bool](Get-NetTCPConnection -LocalPort $cfg.erpPort -State Listen -ErrorAction SilentlyContinue)) {
            Write-Host "'$envName' is up: http://localhost:$($cfg.erpPort)" -ForegroundColor Green
            return
        }
        Start-Sleep -Milliseconds 500
    }
    throw "'$envName' didn't come up within 5s on port $($cfg.erpPort) - check $logDir\erp-server.err.log"
}

function Stop-Environment($envName) {
    $cfg = $envConfig.$envName
    Get-NetTCPConnection -LocalPort $cfg.erpPort -State Listen -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue
    }
}

if ($Rollback) {
    if (-not $Env) { throw "-Rollback requires -Env <test|live>" }
    $worktreePath = Get-WorktreePath $Env
    $prevCommit = & "$PgBin\psql.exe" -h localhost -p $PgPort -U postgres -d $ControlPlaneDb -tAc `
        "SELECT git_commit FROM public.deployments WHERE environment = '$Env' AND build_status = 'passed' ORDER BY promoted_at DESC OFFSET 1 LIMIT 1"
    $prevCommit = $prevCommit.Trim()
    if (-not $prevCommit) { throw "No prior successful deployment recorded for '$Env' to roll back to" }

    Write-Host "Rolling back '$Env' to $prevCommit..." -ForegroundColor Yellow
    Stop-Environment $Env
    & git -C $worktreePath checkout --detach $prevCommit
    Push-Location $worktreePath
    try { & "$GoBin\go.exe" build -ldflags="-s -w" -o erp-server.exe ./cmd/server } finally { Pop-Location }
    Start-Environment $Env $worktreePath
    Add-Deployment -environment $Env -commit $prevCommit -appVersion "" -status "rolled_back" -notes "Rollback to previous deployment"
    exit
}

if (-not $From -or -not $To) { throw "Usage: promote.ps1 -From <dev|test|live> -To <test|live>  (or -Rollback -Env <test|live>)" }

$fromPath = Get-WorktreePath $From
$toPath = Get-WorktreePath $To

# 1. Refuse to promote a dirty tree - what gets promoted must be exactly
# what's committed, nothing uncommitted riding along silently.
$dirty = & git -C $fromPath status --porcelain
if ($dirty) {
    Write-Host "Refusing to promote: '$From' ($fromPath) has uncommitted changes." -ForegroundColor Red
    exit 1
}
$commit = (& git -C $fromPath rev-parse HEAD).Trim()
$shortCommit = (& git -C $fromPath rev-parse --short HEAD).Trim()
Write-Host "Promoting $From -> $To @ $shortCommit" -ForegroundColor Magenta

# 2. Build/vet/test gate - a red build cannot be promoted. Uses -p 1 to
# avoid the known cross-package DB-race false-failure (see
# docs/micro_checklist.md's Stage 14 testing note) rather than mask a real one.
Push-Location $fromPath
try {
    & "$GoBin\go.exe" build ./... 2>&1 | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    & "$GoBin\go.exe" vet ./... 2>&1 | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
    & "$GoBin\go.exe" test ./... -p 1 2>&1 | Write-Host
    if ($LASTEXITCODE -ne 0) {
        Add-Deployment -environment $To -commit $shortCommit -appVersion "" -status "failed" -notes "go test failed - promotion refused"
        throw "go test failed - promotion refused"
    }
} finally {
    Pop-Location
}

# 3. Check the exact commit out into the target's own worktree.
if (-not (Test-Path $toPath)) {
    Write-Host "Creating worktree for '$To' at $toPath..." -ForegroundColor Cyan
    New-Item -ItemType Directory -Path (Split-Path $toPath -Parent) -Force -ErrorAction SilentlyContinue | Out-Null
    & git -C $RepoRoot worktree add $toPath $commit
} else {
    & git -C $toPath fetch $RepoRoot HEAD 2>$null | Out-Null
    & git -C $toPath checkout --detach $commit
}

# 4. Ensure the target database exists and is migrated up to date.
$toCfg = $envConfig.$To
Initialize-Database $toCfg.database
Write-Host "Applying migrations to '$($toCfg.database)'..." -ForegroundColor Cyan
# Run from the target worktree (checked out at the promoted commit above), so
# the migrations applied are exactly the ones that commit ships - not whatever
# happens to be in the dev tree right now.
Invoke-PendingMigrations $toCfg.database $toPath

# 5. Build the stripped release binary in the target worktree.
Push-Location $toPath
try {
    $buildTimestamp = (Get-Date -AsUTC -Format "yyyy-MM-ddTHH:mm:ssZ")
    & "$GoBin\go.exe" build -ldflags="-s -w -X custom_erp/internal/server.gitCommit=$shortCommit -X custom_erp/internal/server.buildTime=$buildTimestamp" -o erp-server.exe ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "release build failed in target worktree" }
} finally {
    Pop-Location
}
$appVersion = (Get-Content (Join-Path $toPath "internal\server\VERSION") -Raw).Trim()

# 6. Restart the target environment on the new binary. DEPLOY-0208 ("Health
# check failed after deployment"): Start-Environment already polls the port
# for 5s and throws if it never comes up - previously that throw just
# crashed the script with nothing recorded in public.deployments at all, so
# a failed promotion and a health-check failure were indistinguishable in
# the deployment history. Record it as "failed" before re-throwing.
Stop-Environment $To
try {
    Start-Environment $To $toPath
} catch {
    Add-Deployment -environment $To -commit $shortCommit -appVersion $appVersion -status "failed" -notes "Health check failed: $($_.Exception.Message)"
    throw
}

# 7. Record the deployment.
Add-Deployment -environment $To -commit $shortCommit -appVersion $appVersion -status "passed" -notes "Promoted from $From"
Write-Host "Promotion complete: $To is now running $shortCommit (v$appVersion)." -ForegroundColor Green
