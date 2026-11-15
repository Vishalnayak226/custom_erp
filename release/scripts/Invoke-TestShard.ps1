# release/scripts/Invoke-TestShard.ps1 - one shard of the release's test run on
# a GitHub machine (item 177.1). .github/workflows/release.yml starts one per
# shard. Each takes its share of one package's tests, split by
# Split-TestsByTime exactly as the laptop's G10 splits them, runs them on the
# machine's own database, reruns a failure alone up to -Reruns times (a pass
# then counts as flaky, as in G10) and writes each test's time for the next
# split. Exit 0 only when every test in the share passed.
param(
    [Parameter(Mandatory)][string]$Exe,   # the compiled test program (go test -c)
    [Parameter(Mandatory)][string]$Dir,   # the package folder it runs in
    [Parameter(Mandatory)][int]$Shard,    # 1..Shards
    [Parameter(Mandatory)][int]$Shards,
    [string]$Times = '',
    [int]$Reruns = 2,
    [string]$Timeout = '40m',
    [string]$OutDir = '.'
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/ReleaseCommon.ps1"
if (-not $Times) { $Times = Join-Path $script:ReleaseDir 'test-times.json' }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$OutDir = (Resolve-Path $OutDir).Path
$Exe = (Resolve-Path $Exe).Path
$pkg = (Split-Path $Dir -Leaf)
Push-Location $Dir
try {
    $names = @(& $Exe '-test.list' '.*' | Where-Object { $_ -match '^(Test|Example|Fuzz)\w*$' })
    if ($LASTEXITCODE -ne 0) { throw "$Exe -test.list failed (exit $LASTEXITCODE)" }
    $split = Split-TestsByTime -Names $names -Times (Read-TestTimes $Times) -Groups $Shards
    $mine = @($split.Buckets[$Shard - 1])
    Write-Host ("{0} shard {1}/{2}: {3} of {4} tests, about {5:n0}s by the last run's times" -f $pkg, $Shard, $Shards, $mine.Count, $names.Count, $split.Load[$Shard - 1])
    if (-not $mine.Count) { exit 0 }

    $log = Join-Path $OutDir "$pkg.log"
    & $Exe '-test.count=1' "-test.timeout=$Timeout" '-test.v' '-test.run' "^($($mine -join '|'))$" 2>&1 | ForEach-Object { "$_" } | Tee-Object -FilePath $log
    $code = $LASTEXITCODE
    $lines = @(Get-Content -LiteralPath $log)

    $seen = @{}
    foreach ($l in $lines) { if ($l -match '^--- (PASS|FAIL|SKIP): (\w+) \(([\d.]+)s\)') { $seen[$Matches[2]] = [double]$Matches[3] } }
    $seen | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $OutDir "times-$pkg.json") -Encoding utf8
    if ($code -eq 0) { exit 0 }

    if ($lines | Where-Object { $_ -match '^panic:' }) {
        Write-Host "::error::$pkg shard $Shard panicked or timed out - the tests after it in this share did not run (see the log above)"
        exit 1
    }
    $failed = @(foreach ($l in $lines) { if ($l -match '^\s*--- FAIL: (\w+)') { $Matches[1] } }) | Select-Object -Unique
    if (-not $failed.Count) {
        # The process died mid-test (log.Fatal, os.Exit): name the test that was running.
        $last = @(foreach ($l in $lines) { if ($l -match '^=== RUN\s+(\S+)') { $Matches[1] } }) | Select-Object -Last 1
        Write-Host "::error::$pkg shard $Shard stopped (exit $code) inside $last - the tests after it in this share did not run (see the log above)"
        exit 1
    }
    $still = @()
    foreach ($t in $failed) {
        $passed = $false
        for ($r = 1; $r -le $Reruns -and -not $passed; $r++) {
            & $Exe '-test.count=1' "-test.timeout=$Timeout" '-test.run' "^$t$" 2>&1 | Out-Null
            $passed = ($LASTEXITCODE -eq 0)
        }
        if ($passed) { Write-Host "::warning::flaky: $pkg $t failed in the shard and passed alone" }
        else { $still += $t; Write-Host "::error::$pkg $t failed, and again alone $Reruns time(s)" }
    }
    if ($env:GITHUB_STEP_SUMMARY) {
        $sum = @("### $pkg shard $Shard") + @($failed | ForEach-Object { if ($_ -in $still) { "- FAIL ``$_``" } else { "- flaky ``$_`` (passed alone)" } })
        $sum | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY
    }
    exit $(if ($still.Count) { 1 } else { 0 })
} finally {
    Pop-Location
}
