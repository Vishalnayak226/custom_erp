# Rebuilds the Knowledge Center (internal/kb/content/) from docs/kb/**/*.md.
#
#   pwsh docs/kb/update-kb.ps1           # rebuild and copy the generated output in
#   pwsh docs/kb/update-kb.ps1 -Check    # do not write; exit non-zero if it is stale
#
# Why this wrapper exists rather than just `go run ./cmd/genkb`:
# Windows Controlled Folder Access (Defender's ransomware protection, on by
# default) refuses any write under %USERPROFILE%\Documents from a binary it does
# not recognise - and a freshly compiled Go binary is never recognised. It
# reports that refusal as "the system cannot find the file specified", which
# looks like a missing directory and is not. So: generate into TEMP with Go,
# copy into place with PowerShell, which Windows already trusts. Same convention
# as docs/brain/update-brain.ps1.
#
# The generated output is COMMITTED. It is embedded into the server binary with
# go:embed, so a build from a clean checkout must find it there - and committing
# it is what makes -Check a meaningful drift signal in the build gate.
#
# -Check is what belongs in CI: it rebuilds in memory, compares against what is
# committed, and fails naming every stale, missing or orphaned file.

[CmdletBinding()]
param(
    [switch]$Check
)

$ErrorActionPreference = "Stop"

$kbDir     = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot  = Split-Path -Parent (Split-Path -Parent $kbDir)
$contentDir = Join-Path $repoRoot "internal\kb\content"

Push-Location $repoRoot
try {
    if ($Check) {
        Write-Host "==> go run ./cmd/genkb -check" -ForegroundColor Cyan
        & go run ./cmd/genkb -check
        if ($LASTEXITCODE -ne 0) {
            Write-Host "Knowledge Center output is out of date. Run: pwsh docs/kb/update-kb.ps1" -ForegroundColor Red
            exit 1
        }
        Write-Host "Knowledge Center is current." -ForegroundColor Green
        exit 0
    }

    $staging = Join-Path ([System.IO.Path]::GetTempPath()) ("genkb-" + [Guid]::NewGuid().ToString("N").Substring(0, 8))
    New-Item -ItemType Directory -Path $staging -Force | Out-Null

    try {
        Write-Host "==> go run ./cmd/genkb -out <temp>" -ForegroundColor Cyan
        & go run ./cmd/genkb -out $staging
        if ($LASTEXITCODE -ne 0) { throw "genkb failed" }

        if (-not (Test-Path (Join-Path $staging "index.json"))) {
            throw "genkb did not produce index.json"
        }

        # Replace rather than merge: an article deleted from docs/kb/ must stop
        # being served, and a leftover HTML file would keep serving it.
        if (Test-Path $contentDir) { Remove-Item -Recurse -Force $contentDir }
        New-Item -ItemType Directory -Path (Join-Path $contentDir "articles") -Force | Out-Null
        Copy-Item -Path (Join-Path $staging "*") -Destination $contentDir -Recurse -Force

        $articles = (Get-ChildItem (Join-Path $contentDir "articles") -Filter *.html).Count
        Write-Host "    wrote internal/kb/content ($articles articles)" -ForegroundColor Green
        Write-Host "    the content is embedded in the binary - rebuild the server to serve it." -ForegroundColor DarkGray
    }
    finally {
        Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
    }
}
finally {
    Pop-Location
}
