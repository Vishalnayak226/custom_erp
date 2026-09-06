# -Check never refreshes the graph or copies outputs.
[CmdletBinding()]
param([switch]$Check, [switch]$SkipGraph)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not $Check -and -not $SkipGraph) {
    Push-Location $repoRoot
    try {
        if (Get-Command graphify -ErrorAction SilentlyContinue) {
            & graphify update .
            if ($LASTEXITCODE -ne 0) { throw 'graphify update failed' }
        }
    } finally { Pop-Location }
}
& (Join-Path (Split-Path -Parent $PSScriptRoot) 'update-docs.ps1') -Group Brain -Check:$Check
