# Compatibility entrypoint; all generated outputs use the shared transaction.
[CmdletBinding()]
param([switch]$Check)
& (Join-Path (Split-Path -Parent $PSScriptRoot) 'update-docs.ps1') -Group Guides -Check:$Check
