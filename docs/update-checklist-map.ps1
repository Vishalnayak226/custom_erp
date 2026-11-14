#Requires -Version 7
<#
.SYNOPSIS
  Refreshes the Module map in docs/micro_checklist.md and the empty-sub-module markers in
  both tracker files, and checks that the live tracker and its archive share one module skeleton.

.DESCRIPTION
  The tracker is arranged as permanent modules (## CODE — Name) split into sub-modules
  (### CODE › Name) that hold the items. The archive has the same skeleton. This script:
    - counts every checkbox line ([ ] open, [x] built; sub-items included) per sub-module,
    - rewrites the table between the module-map markers in the live file,
    - puts "*Nothing open.*" (live) / "*Nothing archived yet.*" (archive) into every empty
      sub-module and removes it from any sub-module that has content,
    - fails when the two skeletons differ, when the archive holds an open item, or when a
      checkbox sits outside the module sections (Asks log / Background notes take no items).
  -Check writes nothing and exits 1 if anything would change or is wrong.

.EXAMPLE
  pwsh docs/update-checklist-map.ps1
  pwsh docs/update-checklist-map.ps1 -Check
#>
param([switch]$Check)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$livePath = Join-Path $root 'docs/micro_checklist.md'
$archPath = Join-Path $root 'docs/archive/micro_checklist_closed_stages.md'

$reMod = '^## ([A-Z]+) — (.+)$'
$reSub = '^### ([A-Z]+) › (.+)$'
$reOpen = '^\s*[-*]\s*\[ \]'
$reDone = '^\s*[-*]\s*\[[xX]\]'
$problems = [System.Collections.Generic.List[string]]::new()

function Read-Lines($path) {
    $raw = [IO.File]::ReadAllText($path)
    if ($raw.Contains("`r`n")) { throw "$path has CRLF line endings; the trackers are LF." }
    return ,([System.Collections.Generic.List[string]]$raw.Split("`n"))
}

function Get-Slug([string]$t) {
    $t = $t.ToLowerInvariant() -replace '[^\p{L}\p{Nd}\p{Mn}\p{Pc}\- ]', ''
    return $t.Replace(' ', '-')
}

# Parses the module part of a file: returns the skeleton (ordered) and per-sub counts.
function Read-Modules($lines, $label) {
    $mods = [ordered]@{}; $curMod = $null; $curSub = $null
    $open = @{}; $done = @{}; $end = $lines.Count
    for ($i = 0; $i -lt $lines.Count; $i++) {
        $l = $lines[$i]
        if ($l -match $reMod) {
            $curMod = $Matches[1]; $curSub = $null
            if (-not $mods.Contains($curMod)) { $mods[$curMod] = @{ Name = $Matches[2]; Subs = [System.Collections.Generic.List[string]]::new() } }
            continue
        }
        if ($l.StartsWith('## ')) {
            if ($curMod) { $end = $i; break }   # first non-module ## after the modules ends the part
            continue
        }
        if ($l -match $reSub) {
            if ($Matches[1] -ne $curMod) { $problems.Add("${label}: '$l' sits under module $curMod") }
            $curSub = "$curMod|$($Matches[2])"; $mods[$curMod].Subs.Add($Matches[2]); continue
        }
        if ($l -match $reOpen -or $l -match $reDone) {
            if (-not $curSub) { $problems.Add("${label} line $($i+1): item outside a sub-module"); continue }
            if ($l -match $reOpen) { $open[$curSub]++ } else { $done[$curSub]++ }
        }
    }
    for ($i = $end; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match $reOpen -or $lines[$i] -match $reDone) {
            $problems.Add("${label} line $($i+1): checkbox outside the module sections (items go in a sub-module)")
        }
    }
    return @{ Mods = $mods; Open = $open; Done = $done; End = $end }
}

# Puts the empty marker into empty sub-modules and takes it out of filled ones.
function Set-Markers($lines, $marker, $end) {
    $out = [System.Collections.Generic.List[string]]::new()
    $i = 0
    while ($i -lt $lines.Count) {
        $l = $lines[$i]; $out.Add($l)
        if ($i -lt $end -and $l -match $reSub) {
            $j = $i + 1
            while ($j -lt $end -and -not ($lines[$j] -match '^#{2,3} ')) { $j++ }
            $body = $lines.GetRange($i + 1, $j - $i - 1)
            $content = @($body | Where-Object { $_.Trim() -ne '' -and $_.Trim() -ne $marker })
            if ($content.Count -eq 0) {
                $out.Add(''); $out.Add($marker); $out.Add('')
            } else {
                # drop the marker and the one blank line it leaves doubled; item text is copied as is
                for ($k = 0; $k -lt $body.Count; $k++) {
                    if ($body[$k].Trim() -ne $marker) { $out.Add($body[$k]); continue }
                    if ($out.Count -and $out[$out.Count - 1].Trim() -eq '' -and $k + 1 -lt $body.Count -and $body[$k + 1].Trim() -eq '') { $k++ }
                }
            }
            $i = $j; continue
        }
        $i++
    }
    return ,$out
}

$L = Read-Lines $livePath
$A = Read-Lines $archPath
$lm = Read-Modules $L 'micro_checklist.md'
$am = Read-Modules $A 'archive'

# Same skeleton in both files.
$ls = ($lm.Mods.Keys | ForEach-Object { "$_ — $($lm.Mods[$_].Name): " + ($lm.Mods[$_].Subs -join ' | ') }) -join "`n"
$as = ($am.Mods.Keys | ForEach-Object { "$_ — $($am.Mods[$_].Name): " + ($am.Mods[$_].Subs -join ' | ') }) -join "`n"
if ($ls -ne $as) {
    $lsA = $ls.Split("`n"); $asA = $as.Split("`n")
    for ($k = 0; $k -lt [Math]::Max($lsA.Count, $asA.Count); $k++) {
        if ($lsA[$k] -ne $asA[$k]) { $problems.Add("skeleton differs - live: '$($lsA[$k])' / archive: '$($asA[$k])'"); break }
    }
}
foreach ($k in $am.Open.Keys) { if ($am.Open[$k]) { $problems.Add("archive holds $($am.Open[$k]) open item(s) in $($k.Replace('|', ' › '))") } }

# Module map table.
$rows = [System.Collections.Generic.List[string]]::new()
$rows.Add('| Module | Open | Built | Open work by sub-module |')
$rows.Add('|---|---:|---:|---|')
$to = 0; $tb = 0
foreach ($code in $lm.Mods.Keys) {
    $m = $lm.Mods[$code]; $o = 0; $b = 0; $where = @()
    foreach ($s in $m.Subs) {
        $key = "$code|$s"
        $so = [int]$lm.Open[$key]; $o += $so; $b += [int]$lm.Done[$key] + [int]$am.Done[$key]
        if ($so) { $where += "$s $so" }
    }
    $to += $o; $tb += $b
    $title = "$code — $($m.Name)"
    $w = if ($where.Count) { $where -join ' · ' } else { '—' }
    $rows.Add("| [$title](#$(Get-Slug $title)) | $o | $b | $w |")
}
$rows.Add("| **All modules** | **$to** | **$tb** | |")

$start = $L.FindIndex({ param($x) $x.StartsWith('<!-- module-map:start') })
$stop = $L.FindIndex({ param($x) $x.StartsWith('<!-- module-map:end') })
if ($start -lt 0 -or $stop -lt $start) { throw 'module-map markers not found in docs/micro_checklist.md' }
$newL = [System.Collections.Generic.List[string]]::new()
$newL.AddRange($L.GetRange(0, $start + 1)); $newL.AddRange($rows); $newL.AddRange($L.GetRange($stop, $L.Count - $stop))
$lEnd = $lm.End + ($newL.Count - $L.Count)
$newL = Set-Markers $newL '*Nothing open.*' $lEnd
$newA = Set-Markers $A '*Nothing archived yet.*' $am.End

# Next free ask number: one above the highest item id in either file, or the highest
# "(ask N" in a run header - an ask that only rebuilds older items has no N.x item of its own.
$next = 1 + (@($L + $A | ForEach-Object {
    if ($_ -match '^\s*[-*]\s*\[[ xX]\]\s*(?:~~)?\*\*(\d+)[.\s*]') { [int]$Matches[1] }
    foreach ($m in [regex]::Matches($_, '\(ask (\d+)')) { [int]$m.Groups[1].Value }
}) | Measure-Object -Maximum).Maximum

$liveChanged = ($newL -join "`n") -ne ($L -join "`n")
$archChanged = ($newA -join "`n") -ne ($A -join "`n")

foreach ($p in $problems) { Write-Host "  PROBLEM  $p" -ForegroundColor Red }
if ($Check) {
    if ($liveChanged) { Write-Host '  DRIFT    micro_checklist.md: module map or markers out of date' -ForegroundColor Yellow }
    if ($archChanged) { Write-Host '  DRIFT    archive: markers out of date' -ForegroundColor Yellow }
    if ($problems.Count -or $liveChanged -or $archChanged) { Write-Host 'Run: pwsh docs/update-checklist-map.ps1'; exit 1 }
    Write-Host "Checklist map is current: $($lm.Mods.Count) modules, $to open, $tb built. Next ask number: $next."
    exit 0
}
$enc = [Text.UTF8Encoding]::new($false)
if ($liveChanged) { [IO.File]::WriteAllText($livePath, ($newL -join "`n"), $enc) }
if ($archChanged) { [IO.File]::WriteAllText($archPath, ($newA -join "`n"), $enc) }
$what = if ($liveChanged -or $archChanged) { 'Updated.' } else { 'No change.' }
Write-Host "Checklist map: $($lm.Mods.Count) modules, $to open, $tb built. $what Next ask number: $next."
if ($problems.Count) { exit 1 }
