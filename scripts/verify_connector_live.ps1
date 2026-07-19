<#
Stage 17.11: verify one real channel connector (Shopify, BigCommerce, or
Magento) against a real disposable store, one platform at a time, per
docs/operations/connector_live_verification.md.

This script does NOT create the disposable PIM Item/ProductContent itself -
those go through this ERP's own approval-gated PIM Workbench UI (the
existing, already-verified path - see Stage 15/16 in docs/project_ledger.md),
and guessing that request shape here would be a second, untested path to the
same place. What this script owns is the part that's actually specific to
Stage 17.11: wiring a disposable Channel to a real platform, saving real
credentials, triggering a real publish, and reporting the real platform's
own response - then cleaning up everything it created.

Prerequisites (do these once, via the running app, before running this):
  1. Log in as an HR/Admin user (required for channel credential config -
     see internal/server/handlers_pim_pos_finance.go's handleSaveChannelCredential) and complete MFA if
     enrolled. Copy the resulting Bearer token - -Token needs it. This
     script deliberately does not automate login/MFA itself: that would mean
     storing a live TOTP secret in yet another local file for a one-time
     verification script, which is not a trade worth making.
  2. In the PIM Workbench UI, create one disposable test Item (fill
     name/barcode/category/hsn_code/gst_rate) and get its ProductContent to
     Approved for locale "en" (the normal editorial + approval flow). Note
     its item code and its category value.
  3. Create CONNECTOR_CREDENTIALS.local.json at the repo root (gitignored -
     see .gitignore's *.local.json entry) - see
     docs/operations/connector_live_verification.md for the exact shape per platform.

Usage:
  .\scripts\verify_connector_live.ps1 -Platform Shopify -ItemCode TEST-DISPOSABLE-001 -Token <bearer token>

What it does, in order: create a disposable Channel for -Platform, map the
item's ERP category to it (ChannelCategoryMap), POST the real credentials
from CONNECTOR_CREDENTIALS.local.json, check PIM completeness for
item+channel (stops here without touching the real platform if not ready),
queue a publish, poll until Published/Failed, report the platform's own
external id (or error) - then delete the disposable Channel/
ChannelCategoryMap it created (never the operator's Item/ProductContent).
Credential values are never printed - only whether each field is present.
#>

[Diagnostics.CodeAnalysis.SuppressMessageAttribute('PSAvoidUsingPlainTextForPassword', 'CredentialsFile', Justification = 'Holds a file *path*, not a credential value - the actual secrets are read from that file at runtime and never passed as a script parameter.')]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("Shopify", "BigCommerce", "Magento")]
    [string]$Platform,

    [Parameter(Mandatory = $true)]
    [string]$ItemCode,

    [Parameter(Mandatory = $true)]
    [string]$Token,

    [string]$BaseUrl = "http://localhost:8080",

    [string]$CredentialsFile = (Join-Path $PSScriptRoot "..\CONNECTOR_CREDENTIALS.local.json"),

    [switch]$SkipCleanup
)

$ErrorActionPreference = "Stop"
$headers = @{ Authorization = "Bearer $Token"; "Content-Type" = "application/json" }
$stamp = Get-Date -Format "yyyyMMddHHmmss"
$channelCode = "VERIFY-$Platform-$stamp".ToUpper()

function Redact($fields) {
    $out = @{}
    foreach ($k in $fields.Keys) {
        $v = "$($fields[$k])"
        $out[$k] = if ($v.Length -gt 4) { "*" * ($v.Length - 4) + $v.Substring($v.Length - 4) } else { "****" }
    }
    return $out
}

Write-Host "== Stage 17.11 live connector verification: $Platform ==" -ForegroundColor Magenta

if (-not (Test-Path $CredentialsFile)) {
    throw "Credentials file not found: $CredentialsFile. See docs/operations/connector_live_verification.md for the expected shape."
}
$allCreds = Get-Content $CredentialsFile -Raw | ConvertFrom-Json
$platformCreds = $allCreds.$Platform
if (-not $platformCreds) { throw "No '$Platform' entry in $CredentialsFile." }
$credFields = @{}
$platformCreds.PSObject.Properties | ForEach-Object { $credFields[$_.Name] = $_.Value }
Write-Host "Loaded credential fields for ${Platform}: $((Redact $credFields).Keys -join ', ') (values redacted)" -ForegroundColor DarkGray

# 1. Look up the disposable item's category (required for the ChannelCategoryMap step below).
$item = Invoke-RestMethod -Uri "$BaseUrl/api/v1/doc/Item/$ItemCode" -Headers $headers -Method Get
if (-not $item.category) { throw "Item '$ItemCode' has no category set - fill it in via the PIM Workbench first." }
Write-Host "Using item '$ItemCode' (category: $($item.category))" -ForegroundColor Cyan

# 2. Create the disposable Channel.
$channelBody = @{ id = $channelCode; platform = $Platform; default_locale = "en"; name = "Stage 17.11 live verification ($Platform)" } | ConvertTo-Json
Invoke-RestMethod -Uri "$BaseUrl/api/v1/doc/Channel" -Headers $headers -Method Post -Body $channelBody | Out-Null
Write-Host "Created disposable channel '$channelCode'" -ForegroundColor Green

try {
    # 3. Map the item's category to the new channel (CheckPublishReadiness requires this).
    $mapBody = @{ id = "$channelCode-CATMAP"; channel = $channelCode; erp_category = $item.category } | ConvertTo-Json
    Invoke-RestMethod -Uri "$BaseUrl/api/v1/doc/ChannelCategoryMap" -Headers $headers -Method Post -Body $mapBody | Out-Null

    # 4. Save the real platform credentials against the channel (encrypted at rest - engines/connector.go).
    Invoke-RestMethod -Uri "$BaseUrl/api/v1/pim/channels/$channelCode/credentials" -Headers $headers -Method Post -Body ($credFields | ConvertTo-Json) | Out-Null
    Write-Host "Saved credentials for channel '$channelCode'" -ForegroundColor Green

    # 5. Readiness check BEFORE touching the real platform.
    $completeness = Invoke-RestMethod -Uri "$BaseUrl/api/v1/pim/completeness/$ItemCode`?channel=$channelCode&locale=en" -Headers $headers -Method Get
    if ($completeness.score -lt 100) {
        Write-Host "Item is not 100% ready for this channel (score $($completeness.score)):" -ForegroundColor Red
        $completeness.missing_fields | ForEach-Object { Write-Host "  - $_" -ForegroundColor Yellow }
        throw "Fix the item via the PIM Workbench, then re-run. No publish attempt was made against $Platform."
    }
    Write-Host "Item is 100% ready for channel '$channelCode' - proceeding to publish." -ForegroundColor Green

    # 6. Queue the publish and poll for a terminal status. The background
    # worker (engines.StartPublishQueueWorker, internal/server/routes.go) ticks every 10s.
    $publishBody = @{ item_code = $ItemCode; channel = $channelCode } | ConvertTo-Json
    $publishResp = Invoke-RestMethod -Uri "$BaseUrl/api/v1/pim/publish" -Headers $headers -Method Post -Body $publishBody
    $jobID = $publishResp.job_id
    Write-Host "Queued publish job $jobID (status: $($publishResp.status))" -ForegroundColor Cyan

    $finalStatus = $null
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Seconds 3
        $jobStatus = Invoke-RestMethod -Uri "$BaseUrl/api/v1/pim/publish/$jobID" -Headers $headers -Method Get
        Write-Host "  poll $($i+1): status=$($jobStatus.status)" -ForegroundColor DarkGray
        if ($jobStatus.status -in @("Published", "Failed")) { $finalStatus = $jobStatus; break }
    }
    if (-not $finalStatus) { throw "Job $jobID did not reach a terminal status within 60s - check the server log." }

    if ($finalStatus.status -eq "Published") {
        Write-Host "SUCCESS: published to $Platform. External id: $($finalStatus.external_id)" -ForegroundColor Green
        Write-Host "This external id is $Platform's own identifier - it is proof the connector reached the real API, not the stub." -ForegroundColor Green
        Write-Host "Manually remove the resulting test product from the $Platform admin panel - this connector has no delete method (publish-only, by design - see engines/connector.go)." -ForegroundColor Yellow
    } else {
        Write-Host "FAILED: $($finalStatus.error_message)" -ForegroundColor Red
    }
} finally {
    if (-not $SkipCleanup) {
        Write-Host "Cleaning up disposable Channel/ChannelCategoryMap..." -ForegroundColor DarkGray
        try { Invoke-RestMethod -Uri "$BaseUrl/api/v1/doc/ChannelCategoryMap/$channelCode-CATMAP" -Headers $headers -Method Delete | Out-Null } catch {}
        try { Invoke-RestMethod -Uri "$BaseUrl/api/v1/doc/Channel/$channelCode" -Headers $headers -Method Delete | Out-Null } catch {}
        Write-Host "Done. The disposable Item/ProductContent from the prerequisite step were NOT touched - remove those yourself if they were only for this test." -ForegroundColor DarkGray
    } else {
        Write-Host "Skipped cleanup (-SkipCleanup) - channel '$channelCode' and its category map are still in place." -ForegroundColor Yellow
    }
}
