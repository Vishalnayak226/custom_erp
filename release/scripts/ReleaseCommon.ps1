# release/scripts/ReleaseCommon.ps1 - shared by Invoke-Release.ps1, Invoke-Deploy.ps1 and
# Invoke-Cleanup.ps1 (Stage 109). Dot-source it: . "$PSScriptRoot\ReleaseCommon.ps1"
$script:ReleaseDir = Split-Path $PSScriptRoot -Parent      # release\ - the clicks, config.json, hold.txt, reports\
$script:RepoRoot = Split-Path $script:ReleaseDir -Parent   # the repository
# GitHub's Linux runners have no TEMP, and Invoke-GuardTests / Invoke-TestShard load
# this file there: 0.25.0's run died on Join-Path $env:TEMP before a test ran (204.1).
if (-not $env:TEMP) { $env:TEMP = [IO.Path]::GetTempPath().TrimEnd('/', '\') }

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

# Test-CandidateRecord (items 47.17.3 / 49.17.2): is there a passing release-candidate
# record for exactly this commit? Release's G10b writes release\reports\release-candidate-<tree>.json
# for the tree G11 commits, with the previous release commit as its base - so for the
# commit Deploy ships, the tree is its own tree and the base is its parent. `releasegate
# verify` refuses a record for another commit or tree, or one whose verdict is not pass
# (its verdict is fail only for a blocking check; warnings pass). Returns Ok and the lines
# to show; never throws for a missing or failing record.
function Test-CandidateRecord {
    param([string]$Repo, [string]$Target, [string]$ReportDir, [string]$Go, [string]$GoRoot = $Repo)
    $base = (& git -C $Repo rev-parse "$Target^" 2>$null | Select-Object -First 1)
    $tree = (& git -C $Repo rev-parse "$Target^{tree}" 2>$null | Select-Object -First 1)
    if (-not $base -or -not $tree) {
        return [pscustomobject]@{ Ok = $false; Lines = @("cannot read the parent and tree of $Target") }
    }
    $rec = Join-Path $ReportDir "release-candidate-$($tree.Substring(0, 7)).json"
    if (-not (Test-Path -LiteralPath $rec)) {
        return [pscustomobject]@{ Ok = $false; Lines = @("no release-candidate record for tree $($tree.Substring(0, 7)) in $ReportDir - run Release.cmd so this commit goes through gate G10b") }
    }
    $out = Invoke-Native -Exe $Go -Arguments @('run', './cmd/releasegate', 'verify', '-record', $rec, '-commit', $base, '-tree', $tree) -WorkDir $GoRoot -AllowFail
    $ok = ($LASTEXITCODE -eq 0)
    return [pscustomobject]@{ Ok = $ok; Lines = @($out) }
}

# Import-RestoreDrillEvidence (49.17.2): the server's restore drill leaves
# restore-drill-evidence.json beside the backups (deploy/restore_drill.sh); this copies
# it into release\evidence\backup_restore.json through `releasegate record -from`,
# naming the commit just deployed. The next Release ships the file. Returns a one-line
# note; a missing file or a failed ingest is a note, never a stop - the deploy is done.
function Import-RestoreDrillEvidence {
    param([string]$Repo, [string]$Go, [string[]]$RemoteLines, [string]$Commit, [string]$GoRoot = $Repo)
    $text = ($RemoteLines -join "`n").Trim()
    if (-not $text.StartsWith('{')) { return 'no restore-drill record on the server yet - run deploy/restore_drill.sh there (admin guide, Backups)' }
    $tmp = Join-Path $env:TEMP "restore_drill_evidence_$([guid]::NewGuid().ToString('N')).json"
    try {
        Set-Content -LiteralPath $tmp -Value $text -Encoding utf8NoBOM
        $out = Invoke-Native -Exe $Go -Arguments @('run', './cmd/releasegate', 'record', '-root', $Repo, '-from', $tmp, '-commit', $Commit) -WorkDir $GoRoot -AllowFail
        if ($LASTEXITCODE -eq 0) { return "restore drill recorded for the release gate: $($out -join ' ')" }
        return "restore-drill record NOT ingested: $($out -join ' ')"
    } finally { Remove-Item -LiteralPath $tmp -ErrorAction SilentlyContinue }
}

# The code map redraw that Release starts after its push (Invoke-BrainRedraw.ps1,
# item 177.5): where it writes, the files it makes, and how a later click waits
# for one still running.
$script:BrainRedrawDir = Join-Path $env:TEMP 'erp_release_brain'
$script:BrainRedrawFiles = @('docs/brain/brain-map.md', 'docs/brain/brain.html', 'docs/generated/brain-manifest.json')

# Read-BrainRedraw returns the last redraw's status (state, commit, pid, why), or
# $null when none ran. A "running" status whose process has ended is "failed".
function Read-BrainRedraw {
    $path = Join-Path $script:BrainRedrawDir 'status.json'
    if (-not (Test-Path -LiteralPath $path)) { return $null }
    try { $s = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json } catch { return $null }
    if ($s.state -eq 'running') {
        $p = Get-Process -Id $s.pid -ErrorAction SilentlyContinue
        if (-not $p -or $p.StartTime.ToFileTimeUtc() -ne [long]$s.startTicks) {
            $s.state = 'failed'; $s.why = 'the redraw stopped before it finished'
        }
    }
    return $s
}

# Get-GraphifyRun returns the program and arguments that rebuild the code graph in
# the folder they run in: graphify_update.py (graphify plus its speed fix) under the
# Python graphify is installed in (Scripts\graphify.exe -> python.exe), else plain graphify.
function Get-GraphifyRun {
    $exe = (Get-Command graphify -ErrorAction Stop).Source
    $python = Join-Path (Split-Path (Split-Path $exe -Parent) -Parent) 'python.exe'
    if (Test-Path -LiteralPath $python) { return @{ Exe = $python; Arguments = @((Join-Path $PSScriptRoot 'graphify_update.py')) } }
    return @{ Exe = $exe; Arguments = @('update', '.', '--force') }
}

# Wait-BrainRedraw waits up to $Minutes for a running redraw, then returns its status.
function Wait-BrainRedraw([int]$Minutes = 10) {
    $until = (Get-Date).AddMinutes($Minutes)
    while ($true) {
        $s = Read-BrainRedraw
        if (-not $s -or $s.state -ne 'running' -or (Get-Date) -gt $until) { return $s }
        Start-Sleep -Seconds 5
    }
}

# Each test's time in the last run, by name (release\test-times.json, kept in
# git so the GitHub run splits the suite the same way; item 177.1).
function Read-TestTimes([string]$Path) {
    $t = @{}
    if (Test-Path -LiteralPath $Path) {
        try { (Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { $t[$_.Name] = [double]$_.Value } } catch { }
    }
    return $t
}

# The guard tests (release\guard-tests.txt, item 177.7) as an ordered map of
# package folder -> test names. A line it cannot read is an error, not a skip.
# 49.6.3: one line on a host's TLS certificate - days left, issuer, and any
# problem a browser would show. The handshake accepts every certificate so an
# expired or wrong one is still read and reported, not just refused.
function Get-CertificateExpiryNote([string]$HostName, [int]$WarnDays = 21) {
    $tcp = $null; $ssl = $null
    try {
        $tcp = [Net.Sockets.TcpClient]::new()
        if (-not $tcp.ConnectAsync($HostName, 443).Wait(10000)) { return "$HostName certificate: no answer on port 443 within 10 s" }
        $script:certErrors = 'None'
        $ssl = [Net.Security.SslStream]::new($tcp.GetStream(), $false, { param($s, $c, $ch, $e) $script:certErrors = "$e"; $true })
        $ssl.AuthenticateAsClient($HostName)
        $cert = [Security.Cryptography.X509Certificates.X509Certificate2]::new($ssl.RemoteCertificate)
        $days = [int][math]::Floor(($cert.NotAfter.ToUniversalTime() - [datetime]::UtcNow).TotalDays)
        $issuer = ($cert.Issuer -split ',\s*' | Where-Object { $_ -like 'O=*' } | Select-Object -First 1) -replace '^O=', ''
        $line = "$HostName certificate: $days days left (until $($cert.NotAfter.ToString('yyyy-MM-dd')), $issuer)"
        if ($script:certErrors -ne 'None') { $line = "**CERTIFICATE PROBLEM:** $line - $($script:certErrors)" }
        elseif ($days -lt $WarnDays) { $line = "**RENEWAL OVERDUE:** $line - Caddy renews 30 days out; check its log on the server" }
        return $line
    } catch {
        return "$HostName certificate not read: $($_.Exception.GetBaseException().Message)"
    } finally {
        if ($ssl) { $ssl.Dispose() }
        if ($tcp) { $tcp.Dispose() }
    }
}

function Read-GuardTests([string]$Path) {
    $g = [ordered]@{}
    foreach ($l in Get-Content -LiteralPath $Path) {
        if ($l -match '^\s*(#|$)') { continue }
        if ($l -notmatch '^\s*(\S+)\s+((Test|Example|Fuzz)\w+)(\s|$)') { throw "release/guard-tests.txt: cannot read '$l'" }
        if (-not $g.Contains($Matches[1])) { $g[$Matches[1]] = [System.Collections.Generic.List[string]]::new() }
        $g[$Matches[1]].Add($Matches[2])
    }
    return $g
}

# Splits test names into $Groups lists of about equal running time, from each
# test's time in the last run (an unknown test counts as the median): longest
# first, each into the group with the least so far. Round-robin left one group
# 45 s behind the others (2026-10-19). Ties go by name, so every machine that
# makes the split gets the same lists: G10 and each GitHub shard (177.1).
function Split-TestsByTime {
    param([string[]]$Names, [hashtable]$Times, [int]$Groups)
    $median = if ($Times.Count) { @($Times.Values | Sort-Object)[[int]($Times.Count / 2)] } else { 1.0 }
    $cost = @{}
    foreach ($n in $Names) { $cost[$n] = if ($Times.ContainsKey($n)) { [double]$Times[$n] } else { [double]$median } }
    $buckets = @(for ($g = 0; $g -lt $Groups; $g++) { , [System.Collections.Generic.List[string]]::new() })
    $load = [double[]]::new($Groups)
    foreach ($n in ($Names | Sort-Object -CaseSensitive @{ Expression = { $cost[$_] }; Descending = $true }, @{ Expression = { $_ }; Descending = $false })) {
        $k = 0; for ($g = 1; $g -lt $Groups; $g++) { if ($load[$g] -lt $load[$k]) { $k = $g } }
        $buckets[$k].Add($n); $load[$k] += $cost[$n]
    }
    return [pscustomobject]@{ Buckets = $buckets; Load = $load }
}
