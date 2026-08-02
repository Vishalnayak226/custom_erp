<#
.SYNOPSIS
  Regenerates the three generated reference appendices under docs/guides/.

.DESCRIPTION
  Wraps `go run ./cmd/gendocs`, which produces:

    ERROR_CODES.md       from internal/server's error catalog
    REPORT_CATALOG.md    from engines' report registry
    PERMISSION_MATRIX.md from the tenant's role_permissions table

  Run this after adding an error code, registering a report, or changing role
  grants. The three files carry a DO-NOT-EDIT header for the same reason:
  a hand-maintained copy of a list the code owns is stale the day someone
  touches the code, which is the drift Stage 30.3 spent a whole pass undoing.

  WHY THE %TEMP% DANCE: on Windows, Controlled Folder Access (Defender's
  ransomware protection, on by default) refuses any write under Documents\
  from a binary it does not recognise - and a freshly compiled Go binary never
  is. It reports that refusal as ERROR_FILE_NOT_FOUND, so Go says "the system
  cannot find the file specified" about a directory that plainly exists. The
  generator writes into %TEMP% and PowerShell (which IS trusted) copies the
  files in. Exactly the same problem and workaround as docs/brain/update-brain.ps1.

.PARAMETER Database
  Connection string for PERMISSION_MATRIX.md. Defaults to the repo's dev
  instance. Pass an empty string to skip that file and generate only the two
  that need no database.

.PARAMETER Check
  Generate into a temporary directory and compare, without writing. Exits
  non-zero if any committed file is out of date. Suitable for CI.

.EXAMPLE
  pwsh docs/guides/update-guides.ps1
  pwsh docs/guides/update-guides.ps1 -Check
#>
[CmdletBinding()]
param(
    [string]$Database = 'postgres://postgres@localhost:5435/custom_erp?sslmode=disable',
    [switch]$Check
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$guidesDir = Join-Path $repoRoot 'docs/guides'
$stage = Join-Path $env:TEMP ("erp-gendocs-" + [guid]::NewGuid().ToString('N').Substring(0, 8))

New-Item -ItemType Directory -Force -Path $stage | Out-Null
try {
    Push-Location $repoRoot
    try {
        $genArgs = @('run', './cmd/gendocs', '-out', $stage)
        if ($Database) { $genArgs += @('-db', $Database) }
        & go @genArgs
        if ($LASTEXITCODE -ne 0) { throw "gendocs failed with exit code $LASTEXITCODE" }
    }
    finally { Pop-Location }

    $generated = Get-ChildItem -Path $stage -Filter '*.md'
    if (-not $generated) { throw "gendocs produced no files" }

    $stale = @()
    foreach ($file in $generated) {
        $target = Join-Path $guidesDir $file.Name

        if ($Check) {
            if (-not (Test-Path $target)) { $stale += "$($file.Name) (missing)"; continue }
            # Compare on content with the generated-on date line removed - that
            # line changes every run and would make -Check permanently red.
            $strip = { param($p) (Get-Content $p -Raw) -replace '(?m)^> \*\*Generated [^*]+\*\*', '> **Generated**' }
            if ((& $strip $target) -ne (& $strip $file.FullName)) { $stale += $file.Name }
            continue
        }

        Copy-Item -Path $file.FullName -Destination $target -Force
        Write-Host "  updated docs/guides/$($file.Name)"
    }

    if ($Check) {
        if ($stale.Count -gt 0) {
            Write-Host "Out of date: $($stale -join ', ')" -ForegroundColor Red
            Write-Host "Run: pwsh docs/guides/update-guides.ps1" -ForegroundColor Yellow
            exit 1
        }
        Write-Host "All generated guides are up to date." -ForegroundColor Green
    }
}
finally {
    Remove-Item -Recurse -Force $stage -ErrorAction SilentlyContinue
}
