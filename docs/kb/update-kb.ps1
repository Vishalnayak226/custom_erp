# Includes generated Markdown prerequisites before building the embedded KB.
[CmdletBinding()]
param([switch]$Check)
& (Join-Path (Split-Path -Parent $PSScriptRoot) 'update-docs.ps1') -Group KB -Check:$Check
