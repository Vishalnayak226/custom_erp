# Redraws the project brain (docs/brain/BRAIN.md + docs/brain/brain.html).
#
#   pwsh docs/brain/update-brain.ps1              # re-extract the graph, then redraw
#   pwsh docs/brain/update-brain.ps1 -SkipGraph   # redraw from the graph as it stands
#   pwsh docs/brain/update-brain.ps1 -Check       # also fail if a file has no region
#
# Why this wrapper exists rather than just `go run ./cmd/brainmap`:
# Windows Controlled Folder Access (Defender's ransomware protection, on by
# default) refuses any write under %USERPROFILE%\Documents from a binary it does
# not recognise - and a freshly compiled Go binary is never recognised. It
# reports that refusal as "the system cannot find the file specified", which
# looks like a missing directory and is not. So: generate into TEMP with Go,
# copy into place with PowerShell, which Windows already trusts.
#
# On a machine without Controlled Folder Access, `go run ./cmd/brainmap` on its
# own does exactly the same thing and this script is unnecessary.

[CmdletBinding()]
param(
    [switch]$SkipGraph,
    [switch]$Check
)

$ErrorActionPreference = "Stop"

$brainDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent (Split-Path -Parent $brainDir)

Push-Location $repoRoot
try {
    if (-not $SkipGraph) {
        if (Get-Command graphify -ErrorAction SilentlyContinue) {
            Write-Host "==> graphify update . (re-extracting the call graph)" -ForegroundColor Cyan
            graphify update .
        } else {
            Write-Host "graphify not on PATH - drawing from the existing graphify-out/graph.json" -ForegroundColor Yellow
        }
    }

    $staging = Join-Path ([System.IO.Path]::GetTempPath()) ("brainmap-" + [Guid]::NewGuid().ToString("N").Substring(0, 8))
    New-Item -ItemType Directory -Path $staging -Force | Out-Null

    try {
        Write-Host "==> go run ./cmd/brainmap" -ForegroundColor Cyan
        $args = @("run", "./cmd/brainmap", "-out", $staging)
        if ($Check) { $args += "-check" }
        & go @args
        $goExit = $LASTEXITCODE

        foreach ($name in @("BRAIN.md", "brain.html")) {
            $src = Join-Path $staging $name
            if (-not (Test-Path $src)) {
                throw "brainmap did not produce $name"
            }
            Copy-Item -Path $src -Destination (Join-Path $brainDir $name) -Force
            Write-Host "    wrote docs/brain/$name" -ForegroundColor Green
        }

        if ($goExit -ne 0) {
            Write-Host "brainmap reported unclaimed files (see above) - the brain was still redrawn." -ForegroundColor Yellow
            exit $goExit
        }
    } finally {
        Remove-Item -Path $staging -Recurse -Force -ErrorAction SilentlyContinue
    }
} finally {
    Pop-Location
}
