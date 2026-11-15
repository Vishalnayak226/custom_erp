# release/scripts/Invoke-GuardTests.ps1 - the guard tests (item 177.7): the
# tests in release/guard-tests.txt, which read the code and the registers and
# run in seconds. They run before the long tests: as the GitHub run's first job
# (.github/workflows/release.yml, "guards"), and first in the laptop's G10 with
# -LocalTests, so a missed registration stops a release in minutes, not at the
# end of the suite. Exit 0 only when every listed test ran and passed; a listed
# test that no longer exists is a failure, so a rename cannot drop a guard.
param(
    [string]$Root = '',        # the repository; default: the one this script is in
    [string]$Go = 'go',
    [hashtable]$Exes = @{}     # package folder -> its compiled test program (G10 has them); others run through go test
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/ReleaseCommon.ps1"
if (-not $Root) { $Root = $script:RepoRoot }
$guards = Read-GuardTests (Join-Path $Root 'release/guard-tests.txt')
$clock = [Diagnostics.Stopwatch]::StartNew()
$bad = [System.Collections.Generic.List[string]]::new()
$count = 0
foreach ($pkg in @($guards.Keys)) {
    $names = @($guards[$pkg])
    $count += $names.Count
    $run = "^($($names -join '|'))$"
    if ($Exes.ContainsKey($pkg)) {
        Push-Location (Join-Path $Root $pkg)
        try { $out = @(& $Exes[$pkg] '-test.count=1' '-test.v' '-test.run' $run 2>&1 | ForEach-Object { "$_" }) } finally { Pop-Location }
    } else {
        Push-Location $Root
        try { $out = @(& $Go test -count=1 -v -run $run "./$pkg" 2>&1 | ForEach-Object { "$_" }) } finally { Pop-Location }
    }
    $code = $LASTEXITCODE
    $result = @{}
    foreach ($l in $out) { if ($l -match '^--- (PASS|FAIL|SKIP): (\w+) \(') { $result[$Matches[2]] = $Matches[1] } }
    $pkgBad = @()
    foreach ($n in $names) {
        if (-not $result.ContainsKey($n)) { $pkgBad += "$pkg $n did not run - renamed or removed? Update release/guard-tests.txt" }
        elseif ($result[$n] -eq 'FAIL') { $pkgBad += "$pkg $n failed" }
    }
    if ($code -ne 0 -and -not $pkgBad.Count) { $pkgBad += "$pkg guard tests exited $code (build failure or panic)" }
    if ($pkgBad.Count) {
        # The failing package's output, so the reason is in the log without a rerun.
        $out | Select-Object -Last 60 | ForEach-Object { Write-Host "   $_" }
        foreach ($b in $pkgBad) { $bad.Add($b); Write-Host "::error::guard: $b" }
    } else {
        Write-Host ("guard {0}: {1} test(s) passed" -f $pkg, $names.Count)
    }
}
if ($bad.Count) {
    if ($env:GITHUB_STEP_SUMMARY) { (@('### Guard tests failed') + @($bad | ForEach-Object { "- $_" })) | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY }
    Write-Output ("guard tests: {0} of {1} failed in {2:n0}s - {3}" -f $bad.Count, $count, $clock.Elapsed.TotalSeconds, ($bad -join '; '))
    exit 1
}
Write-Output ("guard tests: all {0} passed in {1:n0}s" -f $count, $clock.Elapsed.TotalSeconds)
exit 0
