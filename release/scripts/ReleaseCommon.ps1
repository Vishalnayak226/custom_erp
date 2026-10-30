# release/scripts/ReleaseCommon.ps1 - shared by Invoke-Release.ps1, Invoke-Deploy.ps1 and
# Invoke-Cleanup.ps1 (Stage 109). Dot-source it: . "$PSScriptRoot\ReleaseCommon.ps1"
$script:ReleaseDir = Split-Path $PSScriptRoot -Parent      # release\ - the clicks, config.json, hold.txt, reports\
$script:RepoRoot = Split-Path $script:ReleaseDir -Parent   # the repository

function Get-ReleaseConfig {
    $path = Join-Path $script:ReleaseDir 'config.json'
    $cfg = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    foreach ($name in @('goBin', 'psql')) {
        $cfg.$name = [Environment]::ExpandEnvironmentVariables($cfg.$name)
    }
    return $cfg
}

# Each gate heading carries the time since the click, so a slow gate shows in
# the report and can be tuned (Stage 109.9).
$script:GateClock = [Diagnostics.Stopwatch]::StartNew()
function Write-Gate([string]$name) {
    Write-Host ''
    Write-Host ("== [{0:mm\:ss}] {1}" -f $script:GateClock.Elapsed, $name) -ForegroundColor Cyan
}

# Invoke-Native runs a program, returns its output lines, and throws with the
# tail of that output when it exits non-zero - so a failing gate always says why.
function Invoke-Native {
    param([string]$Exe, [string[]]$Arguments, [string]$WorkDir = '', [switch]$AllowFail)
    if ($WorkDir) { Push-Location $WorkDir }
    try {
        # @() so no output is an empty array, never one $null line.
        $out = @(& $Exe @Arguments 2>&1 | ForEach-Object { "$_" })
        $code = $LASTEXITCODE
    } finally {
        if ($WorkDir) { Pop-Location }
    }
    if ($code -ne 0 -and -not $AllowFail) {
        $tail = ($out | Select-Object -Last 25) -join "`n"
        throw "$Exe $($Arguments -join ' ') failed (exit $code):`n$tail"
    }
    return , @($out)
}

# Tables a migration may seed without touching anything a client entered:
# record-type definitions, permissions, approval rules, number series.
$script:MetadataTables = @('doctype_meta', 'doctype_fields', 'role_permissions', 'approval_rules',
    'prefix_configs', 'schema_migrations', 'module_registry')

# Get-DataChangeFindings lists what a release would change in client data on
# production: data statements in new migrations, backfills added to the
# -migrate path, and new background jobs. It replaces the question Claude asked
# before every deploy (2026-10-18/19: cost centers, item attributes, reason
# codes, stock-location defaults).
#   $MigrationFiles - full paths of the new migration files
#   $AddedGoLines   - "path`tline" for every added line of Go in the change
function Get-DataChangeFindings {
    param([string[]]$MigrationFiles, [string[]]$AddedGoLines)
    $findings = [System.Collections.Generic.List[object]]::new()
    # "ON UPDATE CASCADE", "FOR UPDATE" and trigger clauses ("AFTER INSERT OR
    # UPDATE ON t") are not statements; the lookbehinds and the word list skip them.
    $stmt = '(?i)(?<!\b(ON|FOR|OR)\s+)\b(UPDATE\s+([\w%$."]+)|INSERT\s+INTO\s+([\w%$."]+)|DELETE\s+FROM\s+([\w%$."]+)|TRUNCATE\s+([\w%$."]+)|DROP\s+(TABLE|COLUMN|SCHEMA)\b|ALTER\s+TABLE\s+[\w%$."]+\s+DROP\b)'
    $notTables = @('ON', 'OR', 'OF', 'SET', 'CASCADE', 'NO')
    foreach ($file in $MigrationFiles) {
        $n = 0
        foreach ($line in Get-Content -LiteralPath $file) {
            $n++
            $code = ($line -replace '--.*$', '').Trim()
            if (-not $code) { continue }
            $m = [regex]::Match($code, $stmt)
            if (-not $m.Success) { continue }
            $target = @($m.Groups[3].Value, $m.Groups[4].Value, $m.Groups[5].Value, $m.Groups[6].Value) | Where-Object { $_ } | Select-Object -First 1
            if ($target -and $notTables -contains $target.ToUpper()) { continue }
            $table = if ($target) { ($target -split '\.')[-1].Trim('"') } else { '' }
            $destructive = $m.Groups[5].Success -or $m.Groups[6].Success -or $m.Groups[7].Success -or $m.Groups[2].Value -match '(?i)^ALTER'
            $kind = if ($destructive) { 'DESTRUCTIVE' }
                    elseif ($table -and $script:MetadataTables -contains $table) { 'metadata' }
                    else { 'client data' }
            $findings.Add([pscustomobject]@{ Kind = $kind; File = (Split-Path $file -Leaf); Line = $n; Text = $code })
        }
    }
    foreach ($entry in $AddedGoLines) {
        $path, $text = $entry -split "`t", 2
        if ($path -eq 'cmd/server/main.go' -and $text -match 'AllTenants\(|Backfill') {
            $findings.Add([pscustomobject]@{ Kind = 'backfill on migrate'; File = $path; Line = 0; Text = $text.Trim() })
        } elseif ($path -notmatch '_test\.go$' -and $text -match 'time\.NewTicker\(|time\.Tick\(') {
            $findings.Add([pscustomobject]@{ Kind = 'new background job'; File = $path; Line = 0; Text = $text.Trim() })
        } elseif ($path -notmatch '_test\.go$' -and ($text -match 'retentionDelete\(|DELETE\s+FROM' -or
                ($path -eq 'engines/integration_log_retention.go' -and $text -match '^\s*\{"[a-z_]+",'))) {
            # 2026-10-27 (49.18.2): a sweep that starts deleting a table it never
            # touched (or deletes differently) changes client data at its first
            # tick after the deploy, with no migration to show for it.
            $findings.Add([pscustomobject]@{ Kind = 'sweep deletes'; File = $path; Line = 0; Text = $text.Trim() })
        }
    }
    return , $findings.ToArray()
}

# Get-AddedGoLines turns a unified diff into "path`tline" for each added line
# in a .go file.
function Get-AddedGoLines([string[]]$DiffLines) {
    $file = ''
    foreach ($l in $DiffLines) {
        if ($l -match '^\+\+\+ b/(.+)$') { $file = $Matches[1]; continue }
        if ($file -like '*.go' -and $l -match '^\+(?!\+\+)(.*)$') { "$file`t$($Matches[1])" }
    }
}

function Format-Findings($findings) {
    if (-not $findings -or $findings.Count -eq 0) { return @('  (none)') }
    $findings | Sort-Object Kind, File, Line | ForEach-Object {
        $where = if ($_.Line) { "$($_.File):$($_.Line)" } else { $_.File }
        $text = if ($_.Text.Length -gt 140) { $_.Text.Substring(0, 140) + '...' } else { $_.Text }
        "  [$($_.Kind)] $where  $text"
    }
}

# Get-OverdueComplianceReviews (gate G4e, Stage 47.16.1a) lists the compliance
# matrix rows whose review_by date has passed - one "OVERDUE CM-.." line each,
# nothing when every review is current. The rule is the matrix test's own
# (internal/securityscan TestComplianceMatrixReviewsAreNotOverdue, which runs
# only with ERP_COMPLIANCE_REVIEW_GATE=1), so the build's OVERDUE log and the
# gate never disagree. Throws when the check did not run, so a broken checker
# never lets a release through.
#   $RepoDir    - the tree to judge (the release snapshot)
#   $GoExe      - go.exe
#   $MatrixFile - optional: another copy of the matrix (to try the gate on)
function Get-OverdueComplianceReviews {
    param([string]$RepoDir, [string]$GoExe, [string]$MatrixFile = '')
    $test = 'TestComplianceMatrixReviewsAreNotOverdue'
    $env:ERP_COMPLIANCE_REVIEW_GATE = '1'
    if ($MatrixFile) { $env:ERP_COMPLIANCE_MATRIX = $MatrixFile } else { Remove-Item Env:ERP_COMPLIANCE_MATRIX -ErrorAction SilentlyContinue }
    try {
        $out = Invoke-Native -Exe $GoExe -Arguments @('test', './internal/securityscan/', '-run', "^$test`$", '-count=1', '-v') -WorkDir $RepoDir -AllowFail
        $code = $LASTEXITCODE
    } finally {
        Remove-Item Env:ERP_COMPLIANCE_REVIEW_GATE, Env:ERP_COMPLIANCE_MATRIX -ErrorAction SilentlyContinue
    }
    if ($code -eq 0 -and ($out | Where-Object { $_ -match "^--- PASS: $test " })) { return }
    $overdue = @($out | Where-Object { $_ -match 'OVERDUE CM-' } | ForEach-Object { ($_ -replace '^.*?(OVERDUE CM-)', '$1').Trim() })
    if ($code -ne 0 -and $overdue.Count) { return $overdue }
    throw "the compliance review check failed or did not run (exit $code):`n$(($out | Select-Object -Last 15) -join "`n")"
}
