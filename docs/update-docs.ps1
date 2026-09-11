<#
.SYNOPSIS
  Stage, validate and publish documentation. -Check never writes to the repository.
.DESCRIPTION
  All includes guides, embedded KB and the local graph-dependent brain. CI uses
  -Group Content because graphify-out is an intentionally uncommitted input.
  Publication checks for concurrent edits and rolls back on a copy failure.
  This is a local file transaction, not a crash-atomic filesystem transaction.
#>
[CmdletBinding()]
param(
    [switch]$Check,
    [ValidateSet('All', 'Content', 'Guides', 'KB', 'Brain')][string]$Group = 'All'
)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$stage = Join-Path $tempRoot ('erp-docs-' + [guid]::NewGuid().ToString('N'))
$utf8 = [Text.UTF8Encoding]::new($false)

function Resolve-Output([string]$Root, [string]$Relative) {
    if ([IO.Path]::IsPathRooted($Relative) -or $Relative -match '(^|/)\.\.(/|$)|\\|:') {
        throw "Unsafe output path: $Relative"
    }
    $path = [IO.Path]::GetFullPath((Join-Path $Root $Relative))
    $prefix = [IO.Path]::GetFullPath($Root).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $path.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { throw "Output escapes root: $Relative" }
    $probe = $path
    while ($probe.Length -ge $prefix.Length) {
        if (Test-Path -LiteralPath $probe) {
            if ((Get-Item -LiteralPath $probe).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked output path: $Relative" }
        }
        $probe = Split-Path -Parent $probe
    }
    return $path
}
function Content-Hash([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return 'missing' }
    # All pipeline outputs are UTF-8 text. Normalize checkout line endings only.
    $bytes = $utf8.GetBytes([IO.File]::ReadAllText($Path).Replace("`r`n", "`n"))
    return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
}
function Run-Go([string[]]$Arguments) {
    & go @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Documentation generator failed ($($Arguments[1]))" }
}
function Tree-State {
    $state = [ordered]@{}
    foreach ($dir in @('docs', 'cmd', 'engines', 'internal', 'public')) {
        Get-ChildItem -LiteralPath (Join-Path $repoRoot $dir) -Recurse -File |
            Where-Object { $_.FullName -notmatch '[\\/]dist[\\/]' } |
            Sort-Object FullName | ForEach-Object {
                $rel = [IO.Path]::GetRelativePath($repoRoot, $_.FullName).Replace('\', '/')
                $state[$rel] = "$($_.Length):$($_.LastWriteTimeUtc.Ticks):$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash)"
            }
    }
    foreach ($rel in @('go.mod', 'go.sum', 'graphify-out/graph.json')) {
        $path = Join-Path $repoRoot $rel
        if (Test-Path -LiteralPath $path) { $state[$rel] = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash }
    }
    return ($state | ConvertTo-Json -Compress -Depth 3)
}

New-Item -ItemType Directory -Path $stage | Out-Null
Push-Location $repoRoot
try {
    $before = Tree-State
    $config = Get-Content -LiteralPath 'docs/governance/generation.json' -Raw | ConvertFrom-Json
    $release = (Get-Content -LiteralPath 'internal/server/VERSION' -Raw).Trim()
    $sets = [ordered]@{}
    $sources = @{}
    $generators = @{}
    if ($Group -in @('All', 'Content')) {
        Run-Go @('run', './cmd/doclint', '-root', $repoRoot, '-write-catalog', $stage)
        $sets['governance'] = @('docs/generated/capability-catalog.md', 'docs/generated/requirements-traceability.md')
        $sources['governance'] = @('docs/product/capability-register.json', 'docs/requirements/*.md', 'docs/requirements/modules/*.md', 'cmd/doclint/capabilities.go')
        $generators['governance'] = 'cmd/doclint'
    }
    if ($Group -in @('All', 'Content', 'Guides', 'KB')) {
        Run-Go @('run', './cmd/gendocs', '-source', $repoRoot, '-out', $stage)
        $sets['guides'] = @(Get-ChildItem -LiteralPath (Join-Path $stage 'docs') -Recurse -File | Where-Object { -not $_.FullName.StartsWith((Join-Path $stage 'docs/generated') + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) } | ForEach-Object {
            [IO.Path]::GetRelativePath($stage, $_.FullName).Replace('\', '/')
        } | Sort-Object)
        $sources['guides'] = @('cmd/gendocs/*.go', 'engines/*.go', 'internal/server/*.go', 'docs/project_ledger.md', 'docs/governance/generation.json', 'docs/data/registry-snapshot.json', 'docs/data/business-definitions.json')
        $generators['guides'] = 'cmd/gendocs'
    }
    if ($Group -in @('All', 'Content', 'KB')) {
        $kbSource = Join-Path $stage 'kb-source'
        Copy-Item -LiteralPath (Join-Path $repoRoot 'docs/kb') -Destination $kbSource -Recurse
        foreach ($rel in $sets['guides'] | Where-Object { $_.StartsWith('docs/kb/') }) {
            $target = Join-Path $kbSource $rel.Substring(8)
            Copy-Item -LiteralPath (Join-Path $stage $rel) -Destination $target -Force
        }
        $kbOut = Join-Path $stage 'internal/kb/content'
        Run-Go @('run', './cmd/genkb', '-source', $kbSource, '-out', $kbOut)
        $kbBytes = (Get-ChildItem -LiteralPath $kbOut -Recurse -File | Measure-Object -Property Length -Sum).Sum
        if ($kbBytes -gt 2MB) { throw 'Embedded Knowledge Center exceeds the 2 MiB budget.' }
        if ((Get-Item -LiteralPath (Join-Path $kbOut 'search.json')).Length -gt 250KB) { throw 'Knowledge Center search index exceeds the 250 KiB budget.' }
        $sets['kb'] = @(Get-ChildItem -LiteralPath $kbOut -Recurse -File | ForEach-Object {
            [IO.Path]::GetRelativePath($stage, $_.FullName).Replace('\', '/')
        } | Sort-Object)
        $sources['kb'] = @('docs/kb/**/*.md', 'internal/kb/*.go', 'cmd/genkb/main.go')
        $generators['kb'] = 'cmd/genkb'
        Run-Go @('run', './cmd/genkb', '-source', $kbSource, '-manuals', (Join-Path $repoRoot 'docs/governance/manual-selection.json'), '-release', $release, '-out', (Join-Path $stage 'docs/user'))
        $sets['manuals'] = @(Get-ChildItem -LiteralPath (Join-Path $stage 'docs/user') -File | ForEach-Object { 'docs/user/' + $_.Name } | Sort-Object)
        $sources['manuals'] = @('docs/governance/manual-selection.json', 'docs/kb/**/*.md', 'internal/kb/*.go', 'internal/server/VERSION', 'cmd/genkb/main.go')
        $generators['manuals'] = 'cmd/genkb'
    }
    if ($Group -in @('All', 'Brain')) {
        if (-not (Test-Path -LiteralPath 'graphify-out/graph.json')) {
            throw 'Brain requires graphify-out/graph.json. Refresh it explicitly with graphify update .; use -Group Content for a checkout without a graph.'
        }
        $brainOut = Join-Path $stage 'docs/brain'
        Run-Go @('run', './cmd/brainmap', '-out', $brainOut)
        Run-Go @('run', './cmd/brainmap', '-out', $brainOut, '-check')
        $sets['brain'] = @('docs/brain/BRAIN.md', 'docs/brain/brain.html')
        $sources['brain'] = @('docs/brain/brain.map.json', 'graphify-out/graph.json', 'cmd/brainmap/main.go', 'cmd/brainmap/brain.tmpl.html', 'repository file inventory')
        $generators['brain'] = 'cmd/brainmap'
    }
    $publish = [Collections.Generic.List[string]]::new()
    $problems = [Collections.Generic.List[string]]::new()
    foreach ($name in $sets.Keys) {
        $manifestRel = "docs/generated/$name-manifest.json"
        $manifestPath = Resolve-Output $repoRoot $manifestRel
        $previous = $null
        if (Test-Path -LiteralPath $manifestPath) { $previous = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json }
        # Retired outputs are reported, never silently removed or forgotten.
        foreach ($old in $previous.outputs) {
            $null = Resolve-Output $repoRoot $old.path
            if ($old.path -notin $sets[$name]) { $problems.Add("$($old.path) (orphaned; explicit retirement required)") }
        }
        if ($name -eq 'kb' -and (Test-Path -LiteralPath 'internal/kb/content')) {
            Get-ChildItem -LiteralPath 'internal/kb/content' -Recurse -File | ForEach-Object {
                $rel = [IO.Path]::GetRelativePath($repoRoot, $_.FullName).Replace('\', '/')
                if ($rel -notin $sets[$name]) { $problems.Add("$rel (unregistered generated output)") }
            }
        }
        $entries = @()
        foreach ($rel in $sets[$name]) {
            $null = Resolve-Output $repoRoot $rel
            $entries += [ordered]@{ path = $rel; sha256 = (Content-Hash (Join-Path $stage $rel)); sources = $sources[$name] }
            $publish.Add($rel)
        }
        $manifest = [ordered]@{
            schema_version = 1; generator = $generators[$name]; generator_version = $config.generator_version
            release = $release; scope = $config.scope; schema = 'source registries'; tenant = 'none'
            checksum_encoding = 'utf-8-lf'; outputs = $entries
        }
        $stageManifest = Resolve-Output $stage $manifestRel
        New-Item -ItemType Directory -Path (Split-Path -Parent $stageManifest) -Force | Out-Null
        [IO.File]::WriteAllText($stageManifest, ($manifest | ConvertTo-Json -Depth 8) + "`n", $utf8)
        $publish.Add($manifestRel)
    }
    if ((Tree-State) -cne $before) { throw 'Repository changed during generation. No outputs published; rerun after concurrent edits settle.' }
    if ($problems.Count) { throw ($problems -join "`n") }
    $changed = @($publish | Where-Object { (Content-Hash (Join-Path $repoRoot $_)) -ne (Content-Hash (Join-Path $stage $_)) })
    if ($Check) {
        if ($changed.Count) { throw "Generated drift (no files written):`n$($changed -join "`n")" }
        Write-Host "Documentation is current ($($publish.Count) declared files; no repository writes)."
    } else {
        $applied = [Collections.Generic.List[object]]::new()
        try {
            foreach ($rel in $changed) {
                $target = Resolve-Output $repoRoot $rel
                $backup = Join-Path $stage ('backup/' + $rel)
                $existed = Test-Path -LiteralPath $target
                $stamp = $null
                if ($existed) {
                    $stamp = (Get-Item -LiteralPath $target).LastWriteTimeUtc
                    New-Item -ItemType Directory -Path (Split-Path -Parent $backup) -Force | Out-Null
                    Copy-Item -LiteralPath $target -Destination $backup
                }
                New-Item -ItemType Directory -Path (Split-Path -Parent $target) -Force | Out-Null
                $applied.Add(@{ Target = $target; Backup = $backup; Existed = $existed; Stamp = $stamp })
                Copy-Item -LiteralPath (Join-Path $stage $rel) -Destination $target -Force
            }
        } catch {
            for ($i = $applied.Count - 1; $i -ge 0; $i--) {
                $entry = $applied[$i]
                if ($entry.Existed) {
                    if ((Get-FileHash -LiteralPath $entry.Backup).Hash -ne (Get-FileHash -LiteralPath $entry.Target).Hash) {
                        Copy-Item -LiteralPath $entry.Backup -Destination $entry.Target -Force
                    }
                    if ((Get-Item -LiteralPath $entry.Target).LastWriteTimeUtc -ne $entry.Stamp) {
                        [IO.File]::SetLastWriteTimeUtc($entry.Target, $entry.Stamp)
                    }
                } elseif (Test-Path -LiteralPath $entry.Target) { Remove-Item -LiteralPath $entry.Target -Force }
            }
            throw
        }
        Write-Host "Published $($changed.Count) changed documentation files after all generators passed."
    }
} finally {
    Pop-Location
    $resolvedStage = [IO.Path]::GetFullPath($stage)
    if ($resolvedStage.StartsWith($tempRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path -Leaf $resolvedStage) -like 'erp-docs-*') {
        Remove-Item -LiteralPath $resolvedStage -Recurse -Force
    }
}
