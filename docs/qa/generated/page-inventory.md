# Page and action inventory

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `public/**/*.js`, the shell's view registry in `public/app.js`, `internal/server/routes.go` and `route_capabilities.go`, role templates, the data and settings registries, and the owner table in `docs/product/wholeops-outsider-plan-2026-10-23.md`
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `public/**/*.js`, the shell's view registry in `public/app.js`, `internal/server/routes.go` and `route_capabilities.go`, role templates, the data and settings registries, and the owner table in `docs/product/wholeops-outsider-plan-2026-10-23.md`, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

Stage 135.5. Each front-end file, the screens the shell routes to it, and each API call its source makes, with the handler, module / feature gate, capability and roles enforced for that route. **Roles** are evaluated with `checkRouteCapability`, the check every signed-in request passes, for: Super Admin, Admin, Administrator, Store Supervisor, Cashier, Picker, Warehouse Manager, Accounts Payable, Accounts Receivable, Accountant, HR Manager, Employee Self-Service, Auditor, Integrator, Setup Operator, Privacy Approver, Merchandiser. A module gate also needs the module switched on for the client, and generic record routes also need the role's per-doctype grant. **Methods** are every method registered for the called path; the source is not parsed for which one it sends, and a path built from a base is matched to every route under that base. **Nothing here was inspected live** and tabs are not derived from source: 55.0F records both. **Failure behaviour** is one shared contract for every screen (47.10.6): a refused permission shows *No access*, a missing record *Not found*, any other load failure *Nothing was changed. Trying again is safe.* with a retry.

| Count | |
|---|---|
| Front-end files with screens or API calls | 138 |
| Screens in the shell registry | 79 |
| File-to-route actions | 1287 |
| Registered `/api` routes | 1121 |
| Routes no front-end file calls | 216 |
| Calls matching no route | 4 |

## Screens and front-end files

### `public/app.js`

Screens: home (`#/view/home`; help: Finding your way around, verified 2026-09-24; Keyboard shortcuts and quick codes, verified 2026-10-26; Custom dashboards, verified 2026-10-22).  
Owners: Shared contract across M01–M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/approval/pending` | `handleListPendingApprovals` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/auth/forgot-password` | `handleForgotPassword` |  | public: auth.default | anyone |
| POST | `/api/v1/auth/logout` | `handleLogout` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/auth/mfa/activate` | `handleMFAActivate` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/auth/mfa/enroll` | `handleMFAEnroll` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/auth/mfa/verify` | `handleMFAVerify` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/auth/reset-password` | `handleResetPassword` |  | public: auth.default | anyone |
| GET | `/api/v1/availability` | `handleGetAvailability` |  | signed-in: availability.default | every signed-in role |
| GET | `/api/v1/branding` | `handleBranding` |  | public: version.default | anyone |
| POST | `/api/v1/checkout` | `handleCheckout` |  | signed-in: checkout.default | every signed-in role |
| GET | `/api/v1/data-quality` | `handleListDataQuality` |  | signed-in: master.data-quality | every signed-in role |
| GET | `/api/v1/doc-find` | `handleDocFind` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/grn-steps` | `handleGRNStepsInbox` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/heals` | `handleListHeals` |  | signed-in: master.data-healer | every signed-in role |
| GET | `/api/v1/health` | `handleHealth` |  | public: health.default | anyone |
| GET | `/api/v1/images` | `handleListOwnerImages` |  | signed-in: images.default | every signed-in role |
| POST | `/api/v1/images` | `handleUploadOwnerImage` |  | signed-in: images.default | every signed-in role |
| GET | `/api/v1/import-batches` | `handleImportBatchList` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches` | `handleImportBatchCreate` |  | signed-in: import.default | every signed-in role |
| GET | `/api/v1/inbox` | `handleTaskInbox` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/jobs` | `handleListJobs` |  | signed-in: jobs.view | Super Admin, Admin, Administrator, Auditor |
| ANY | `/api/v1/labels` | `handleLabels` |  | signed-in: labels.default | every signed-in role |
| GET | `/api/v1/localization` | `handleGetLocalization` |  | signed-in: localization.default | every signed-in role |
| POST | `/api/v1/login` | `handleLogin` |  | public: login.default | anyone |
| POST | `/api/v1/logs/browser-error` | `handleBrowserError` |  | signed-in: system.default | every signed-in role |
| POST | `/api/v1/me/change-password` | `handleChangePassword` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me/modules` | `handleMyModules` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me/permissions` | `handleMyPermissions` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me` | `handleGetProfile` |  | signed-in: me.default | every signed-in role |
| PUT | `/api/v1/me` | `handleUpdateProfile` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/meta/doctypes` | `handleGetDocTypes` |  | signed-in: meta.default | every signed-in role |
| POST | `/api/v1/meta/doctypes` | `handleSaveDocType` |  | signed-in: meta.default | every signed-in role |
| GET | `/api/v1/meta/field-formats` | `handleFieldFormats` |  | signed-in: meta.default | every signed-in role |
| POST | `/api/v1/orders` | `handleCreateSalesOrder` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/pos/offline-heartbeat` | `handlePOSOfflineHeartbeat` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/till-series` | `handlePOSTillSeries` |  | signed-in: pos.default | every signed-in role |
| ANY | `/api/v1/prefix` | `handlePrefix` |  | signed-in: prefix.default | every signed-in role |
| GET | `/api/v1/ready` | `handleReadiness` |  | public: health.default | anyone |
| GET | `/api/v1/reason-codes` | `handleListReasonCodes` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/reserve` | `handleCreateReservation` |  | signed-in: reserve.default | every signed-in role |
| POST | `/api/v1/returns` | `handleCreateReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| ANY | `/api/v1/sequence` | `handleSequence` |  | signed-in: sequence.default | every signed-in role |
| GET | `/api/v1/setup/status` | `handleSetupStatus` |  | signed-in: setup.default | every signed-in role |
| GET | `/api/v1/stock-age-inspections` | `handleStockAgeInspections` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/stock-age-inspections` | `handleStockAgeInspections` | quality | signed-in: quality.default | every signed-in role |
| GET | `/api/v1/stock-locations` | `handleStockLocations` |  | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/stock-locations` | `handleStockLocations` |  | admin: admin.stock_locations | Super Admin, Admin |
| GET | `/api/v1/system/environment` | `handleGetEnvironment` |  | signed-in: system.default | every signed-in role |
| GET | `/api/v1/version` | `handleVersion` |  | public: version.default | anyone |

### `public/attachments-panel.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| DELETE | `/api/v1/attachments/file/{attachmentID}` | `handleRemoveAttachment` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/attachments/file/{attachmentID}` | `handleDownloadAttachment` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/attachments/{doctype}/{id}` | `handleListAttachments` |  | signed-in: attachments.default | every signed-in role |
| POST | `/api/v1/attachments/{doctype}/{id}` | `handleAddAttachment` |  | signed-in: attachments.default | every signed-in role |

### `public/components/erp-typeahead.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/f/f.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/crm/public/feedback` | `handleCRMPublicFeedback` | crm_loyalty | public: crm_loyalty.default | anyone |

### `public/grn-steps.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/grn-steps/open-pos` | `handleGRNStepsOpenPOs` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps/{id}/charges` | `handleGRNChargeLines` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/correct` | `handleGRNStepsCorrect` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/pay` | `handleGRNStepsPay` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps/{id}/piece-codes` | `handleGRNPieceCodesGet` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/piece-codes` | `handleGRNPieceCodesSave` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/post-all` | `handleGRNStepsPostAll` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/price` | `handleGRNStepsPrice` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/receive` | `handleGRNStepsReceive` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/verify` | `handleGRNStepsVerify` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps/{id}` | `handleGRNStepsGet` | procurement | signed-in: procurement.default | every signed-in role |

### `public/p/p.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/crm/public/preferences` | `handleCRMPublicPreferences` | crm_loyalty | public: crm_loyalty.default | anyone |

### `public/p/vendor.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/procurement/vendor-link/public` | `handleVendorLinkPublic` | procurement | public: procurement.default | anyone |

### `public/passkey-client.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/auth/passkey/options` | `handlePasskeyAssertOptions` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/auth/passkey/verify` | `handlePasskeyAssertVerify` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/me/passkeys/options` | `handlePasskeyRegisterOptions` |  | signed-in: me.default | every signed-in role |
| DELETE | `/api/v1/me/passkeys/{id}` | `handleDeleteMyPasskey` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me/passkeys` | `handleListMyPasskeys` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/passkeys` | `handlePasskeyRegisterFinish` |  | signed-in: me.default | every signed-in role |

### `public/pim-catalog-share.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/pim/catalog-share` | `handlePIMCatalogShare` | pim | public: pim.default | anyone |

### `public/print-routing.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/print/qz/assignments` | `handleQZPrinterChoices` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/print/qz/assignments` | `handleQZPrinterChoices` | stickers | signed-in: stickers.default | every signed-in role |

### `public/print-template-studio.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/print/catalog` | `handlePrintCatalog` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/print/document/{txn}/{id}` | `handlePrintDocument` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/print/templates/{id}/versions/{version}` | `handlePrintTemplateVersionAction` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/print/templates/{id}/versions` | `handlePrintTemplateVersions` |  | signed-in: doc.default | every signed-in role |

### `public/qz-print-flow.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/print/qz/log` | `handleQZPrintLog` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/print/qz/log` | `handleQZPrintLog` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/print/qz/payload` | `handleQZPrintPayload` | stickers | signed-in: stickers.default | every signed-in role |

### `public/qz-print.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/print/qz/certificate` | `handleQZCertificate` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/print/qz/sign` | `handleQZSign` | stickers | signed-in: stickers.default | every signed-in role |

### `public/reason-picker.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/reason-codes` | `handleListReasonCodes` |  | signed-in: doc.default | every signed-in role |

### `public/shell-identity.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/branding/logo` | `handleBrandingLogo` |  | public: version.default | anyone |
| GET | `/api/v1/branding` | `handleBranding` |  | public: version.default | anyone |
| GET | `/api/v1/version` | `handleVersion` |  | public: version.default | anyone |

### `public/shortcuts.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc-find` | `handleDocFind` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/inbox` | `handleTaskInbox` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/reports/catalog` | `handleReportCatalog` | reports | signed-in: reports.default | every signed-in role |

### `public/sticker-engine.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/sticker-studio.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/store/store-crm.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/crm/customers/{id}/{part}` | `handleCRMCustomer` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/customers/{id}` | `handleCRMCustomer` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/customers` | `handleCRMCustomers` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/holds` | `handleCRMHolds` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/messages` | `handleCRMMessages` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tasks/{id}/{action}` | `handleCRMTasks` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tasks` | `handleCRMTasks` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/templates` | `handleCRMTemplates` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |

### `public/store/store-earnings.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/store-app/earnings` | `handleStoreAppEarnings` |  | signed-in: store_app.default | every signed-in role |

### `public/store/store-floor.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/pos/held-bills` | `handleListHeldPOSBills` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/held-bills` | `handleHoldPOSBill` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/store-app/orders` | `handleStoreAppOrder` | oms | signed-in: oms.default | every signed-in role |

### `public/store/store-inspect.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/reason-codes` | `handleListReasonCodes` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/store-app/inspections/dispose` | `handleStoreAppInspectionDispose` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/store-app/inspections/result` | `handleStoreAppInspectionResult` | quality | signed-in: quality.default | every signed-in role |
| GET | `/api/v1/store-app/inspections` | `handleStoreAppInspections` | quality | signed-in: quality.default | every signed-in role |

### `public/store/store-stock.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/verify` | `handleGRNStepsVerify` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/store-app/counts` | `handleStoreAppCounts` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/store-app/counts` | `handleStoreAppCounts` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/store-app/labels` | `handleStoreAppLabels` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/store-app/labels` | `handleStoreAppLabels` | stickers | signed-in: stickers.default | every signed-in role |
| GET | `/api/v1/store-app/receive/{kind}/{id}` | `handleStoreAppReceiveDocument` |  | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/store-app/receive` | `handleStoreAppReceiveList` |  | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/store-app/sources` | `handleStoreAppSources` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/receive` | `handleReceiveTransferOrder` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/submit` | `handleSubmitTransferOrder` | inventory | signed-in: inventory.default | every signed-in role |

### `public/store/store-today.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/store-app/counts` | `handleStoreAppCounts` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/store-app/counts` | `handleStoreAppCounts` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/store-app/devices` | `handleStoreAppDevices` |  | signed-in: store_app.devices | every signed-in role |
| POST | `/api/v1/store-app/devices` | `handleStoreAppDevices` |  | signed-in: store_app.devices | every signed-in role |
| GET | `/api/v1/store-app/earnings` | `handleStoreAppEarnings` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/inspections` | `handleStoreAppInspections` | quality | signed-in: quality.default | every signed-in role |
| GET | `/api/v1/store-app/item` | `handleStoreAppItem` |  | signed-in: availability.default | every signed-in role |
| GET | `/api/v1/store-app/labels` | `handleStoreAppLabels` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/store-app/labels` | `handleStoreAppLabels` | stickers | signed-in: stickers.default | every signed-in role |
| GET | `/api/v1/store-app/low-stock` | `handleStoreAppLowStock` | inventory | signed-in: availability.default | every signed-in role |
| POST | `/api/v1/store-app/orders` | `handleStoreAppOrder` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/store-app/pin` | `handleStoreAppPIN` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/store-app/pin` | `handleStoreAppPIN` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/push` | `handleStoreAppPush` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/store-app/push` | `handleStoreAppPush` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/receive` | `handleStoreAppReceiveList` |  | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/store-app/sources` | `handleStoreAppSources` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/store-app/today` | `handleStoreAppToday` | sales | signed-in: store_app.default | every signed-in role |

### `public/store/store.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/decide` | `handleDecideApproval` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/auth/logout` | `handleLogout` |  | signed-in: auth.default | every signed-in role |
| POST | `/api/v1/auth/mfa/verify` | `handleMFAVerify` |  | signed-in: auth.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/inbox` | `handleTaskInbox` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/login` | `handleLogin` |  | public: login.default | anyone |
| GET | `/api/v1/me/stores` | `handleMyStores` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/working-store` | `handleSwitchWorkingStore` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me` | `handleGetProfile` |  | signed-in: me.default | every signed-in role |
| PUT | `/api/v1/me` | `handleUpdateProfile` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/store-app/device/check` | `handleStoreAppDeviceCheck` |  | public: login.default | anyone |
| POST | `/api/v1/store-app/devices/{id}/remove` | `handleStoreAppDeviceRemove` |  | signed-in: store_app.devices | every signed-in role |
| GET | `/api/v1/store-app/devices` | `handleStoreAppDevices` |  | signed-in: store_app.devices | every signed-in role |
| POST | `/api/v1/store-app/devices` | `handleStoreAppDevices` |  | signed-in: store_app.devices | every signed-in role |
| GET | `/api/v1/store-app/earnings` | `handleStoreAppEarnings` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/inspections` | `handleStoreAppInspections` | quality | signed-in: quality.default | every signed-in role |
| GET | `/api/v1/store-app/item` | `handleStoreAppItem` |  | signed-in: availability.default | every signed-in role |
| POST | `/api/v1/store-app/pin/login` | `handleStoreAppPINLogin` |  | public: login.default | anyone |
| POST | `/api/v1/store-app/pin/remove` | `handleStoreAppPINRemove` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/pin` | `handleStoreAppPIN` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/store-app/pin` | `handleStoreAppPIN` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/push/key` | `handleStoreAppPushKey` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/store-app/push/off` | `handleStoreAppPushOff` |  | signed-in: store_app.default | every signed-in role |
| GET | `/api/v1/store-app/push` | `handleStoreAppPush` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/store-app/push` | `handleStoreAppPush` |  | signed-in: store_app.default | every signed-in role |

### `public/trade-docs.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/print/document/{txn}/{id}/log` | `handlePrintDocumentLog` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/print/document/{txn}/{id}/reprint-request` | `handleRequestReprint` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/print/document/{txn}/{id}/send` | `handlePrintDocumentSend` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/print/document/{txn}/{id}` | `handlePrintDocument` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/excel` | `handleVendorPOExcel` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/images` | `handleGetPOImages` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/images` | `handleUploadPOImage` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/print` | `handlePrintPurchaseOrder` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/reconciliation` | `handlePOReconciliation` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/send` | `handleSendPurchaseOrder` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/short-close` | `handlePOShortClose` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/vendor-link` | `handleVendorLinkState` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/vendor-link` | `handleVendorLinkCreate` | procurement | signed-in: procurement.default | every signed-in role |

### `public/txn-screen.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/stock-locations/txn-header` | `handleStockTxnHeader` |  | signed-in: inventory.default | every signed-in role |

### `public/view-admin.js`

Screens: Prefix Configurations (`#/view/prefix-configs`; help: Settings and configuration, verified 2026-11-16; A day as an administrator, verified 2026-08-17), Approval Rules (`#/view/approval-rules`; help: Security, Roles & Approvals, verified 2026-11-15; A day as an administrator, verified 2026-08-17), Dynamic Labels (`#/view/dynamic-labels`; help: Settings and configuration, verified 2026-11-16; A day as an administrator, verified 2026-08-17), Extension Hooks (`#/view/extension-hooks`; help: Settings and configuration, verified 2026-11-16; A day as an administrator, verified 2026-08-17), API access (`#/view/api-access`; help: Settings and configuration, verified 2026-11-16; Actions, reports and webhooks through the API, verified 2026-11-17; Authenticating against the public API, verified 2026-11-16; Reading and writing records through the API, verified 2026-11-16), Extension Hook Log (`#/view/extension-hook-log`; help: Settings and configuration, verified 2026-11-16; A day as an administrator, verified 2026-08-17), Integration Logs (`#/view/audit-logs`; help: Security, Roles & Approvals, verified 2026-11-15; A day as an administrator, verified 2026-08-17), Configuration (`#/view/configuration`; help: Your first sign-in, verified 2026-11-14; CRM & Loyalty, verified 2026-11-15; Finance & Tax, verified 2026-11-15; Product Information Management, verified 2026-09-03; Point of Sale, verified 2026-11-16; Procurement, verified 2026-09-03; Settings and configuration, verified 2026-11-16; Country codes and phone number rules, verified 2026-09-10; A day as an administrator, verified 2026-08-17), System Status (`#/view/system-status`; help: Backup & Restore, verified 2026-09-03; Incident Response & Alerting, verified 2026-09-03; Settings and configuration, verified 2026-11-16; A day as an administrator, verified 2026-08-17), Tenant Entitlements (`#/view/tenant-entitlements`; help: Control Panel and Data Console (platform operators), verified 2026-10-18; A day as an administrator, verified 2026-08-17), Tenant Usage (`#/view/tenant-usage`; help: Control Panel and Data Console (platform operators), verified 2026-10-18; A day as an administrator, verified 2026-08-17), Data Console (`#/view/platform-console`; help: Control Panel and Data Console (platform operators), verified 2026-10-18).  
Owners: M01, M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/extension/hooks/{id}/log` | `handleGetExtensionHookLog` |  | admin: admin.extension | Super Admin |
| DELETE | `/api/v1/admin/extension/hooks/{id}` | `handleDeleteExtensionHook` |  | admin: admin.extension | Super Admin |
| GET | `/api/v1/admin/extension/hooks` | `handleListExtensionHooks` |  | admin: admin.extension | Super Admin |
| POST | `/api/v1/admin/extension/hooks` | `handleCreateExtensionHook` |  | admin: admin.extension | Super Admin |
| POST | `/api/v1/admin/extension/token` | `handleIssueExtensionToken` |  | admin: admin.extension | Super Admin |
| GET | `/api/v1/admin/industries` | `handleGetIndustries` |  | admin: admin.industries | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/industry/lock` | `handleGetIndustryLock` |  | admin: admin.industry | Super Admin, Admin, Setup Operator |
| POST | `/api/v1/admin/industry` | `handleSwitchIndustry` |  | admin: admin.industry | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/packages` | `handleListProductPackages` |  | admin: admin.packages | Super Admin |
| GET | `/api/v1/admin/roles` | `handleListRoles` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles` | `handleCreateRole` |  | admin: admin.roles | Super Admin, Admin |
| GET | `/api/v1/admin/settings` | `handleGetSettings` |  | admin: admin.settings | Super Admin, Admin, Setup Operator |
| PUT | `/api/v1/admin/settings` | `handleUpdateSettings` |  | admin: admin.settings | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/tenant-usage` | `handleTenantUsage` |  | admin: admin.tenant-usage | Super Admin |
| POST | `/api/v1/admin/tenant/module-entitlement` | `handleSetModuleEntitlement` |  | admin: admin.tenant | Super Admin |
| GET | `/api/v1/admin/tenant/module-entitlements` | `handleGetModuleEntitlements` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/tenant/package/impact` | `handlePackageImpact` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/tenant/package` | `handleSetTenantPackage` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/tenant/provision` | `handleProvisionTenant` |  | admin: admin.tenant | Super Admin |
| GET | `/api/v1/admin/tenants` | `handleListTenants` |  | admin: admin.tenants | Super Admin |
| ANY | `/api/v1/approval/rules` | `handleApprovalRules` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/integration/logs` | `handleGetIntegrationLogs` |  | signed-in: integration.default | every signed-in role |
| POST | `/api/v1/integration/retry` | `handleRetryIntegrationEvent` |  | signed-in: integration.default | every signed-in role |
| POST | `/api/v1/integrations/credentials/disconnect` | `handleConnectorDisconnect` |  | admin: admin.integrations | Super Admin, Admin |
| POST | `/api/v1/jobs/{id}/cancel` | `handleCancelJob` |  | signed-in: jobs.manage | Super Admin, Admin, Administrator |
| POST | `/api/v1/jobs/{id}/retry` | `handleRetryJob` |  | signed-in: jobs.manage | Super Admin, Admin, Administrator |
| GET | `/api/v1/jobs` | `handleListJobs` |  | signed-in: jobs.view | Super Admin, Admin, Administrator, Auditor |
| ANY | `/api/v1/labels` | `handleLabels` |  | signed-in: labels.default | every signed-in role |
| ANY | `/api/v1/logs/audit` | `handleAuditLogs` |  | signed-in: audit.logs | Super Admin, Admin, Administrator, Auditor |
| POST | `/api/v1/logs/system/resolve` | `handleResolveSystemFault` |  | admin: admin.patch | Super Admin |
| ANY | `/api/v1/logs/system` | `handleSystemLogs` |  | signed-in: system.logs | Super Admin, Administrator, Auditor |
| GET | `/api/v1/ops/alerting-status` | `handleAlertingStatus` |  | signed-in: ops.default | every signed-in role |
| GET | `/api/v1/ops/backup-status` | `handleBackupStatus` |  | signed-in: ops.default | every signed-in role |
| GET | `/api/v1/ops/deployment-status` | `handleDeploymentStatus` |  | signed-in: ops.default | every signed-in role |
| GET | `/api/v1/pinelabs/credentials` | `handleGetPineLabsCredentials` |  | signed-in: pinelabs.default | every signed-in role |
| POST | `/api/v1/pinelabs/credentials` | `handlePineLabsCredentials` |  | signed-in: pinelabs.default | every signed-in role |
| GET | `/api/v1/platform/me` | `handlePlatformMe` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/attribute-library/rules` | `handleApplyCategoryRules` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/attribute-library` | `handleAttributeLibrary` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/attribute-library` | `handleApplyAttributeLibrary` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/open` | `handleControlPanelOpenTenant` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/query-plan` | `handleControlPanelQueryPlan` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/records/{doctype}/{id}` | `handlePlatformUpdateRecord` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/records/{doctype}` | `handlePlatformRecords` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/records` | `handlePlatformRecordTypes` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/status` | `handleControlPanelTenantStatus` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/tables/{table}/row` | `handlePlatformUpdateRow` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/tables/{table}` | `handlePlatformRows` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/tables` | `handlePlatformTables` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/trial` | `handleControlPanelTenantTrial` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants` | `handlePlatformTenants` |  | admin: platform.console | Super Admin |
| ANY | `/api/v1/prefix` | `handlePrefix` |  | signed-in: prefix.default | every signed-in role |
| GET | `/api/v1/razorpay/credentials` | `handleGetRazorpayCredentials` |  | admin: admin.integrations | Super Admin, Admin |
| POST | `/api/v1/razorpay/credentials` | `handleRazorpayCredentials` |  | admin: admin.integrations | Super Admin, Admin |
| GET | `/api/v1/setup/path/{step}/history` | `handleSetupStepHistory` |  | signed-in: setup.default | every signed-in role |
| POST | `/api/v1/setup/path/{step}/note` | `handleSetupStepNote` |  | admin: admin.setup-path | Super Admin, Admin |
| GET | `/api/v1/setup/path` | `handleSetupPath` |  | signed-in: setup.default | every signed-in role |
| GET | `/api/v1/unicommerce/credentials` | `handleGetUnicommerceCredentials` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/unicommerce/credentials` | `handleUnicommerceCredentials` | oms | signed-in: oms.default | every signed-in role |

### `public/view-ai-access.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/ai-access/clients` | `handleListAIAccessClients` | ai_access | admin: admin.api-credentials | Super Admin, Admin |
| POST | `/api/v1/admin/ai-access/clients` | `handleIssueAIAccessClient` | ai_access | admin: admin.api-credentials | Super Admin, Admin |
| DELETE | `/api/v1/admin/api-credentials/{id}` | `handleRevokeAPICredential` |  | admin: admin.api-credentials | Super Admin, Admin |
| GET | `/api/v1/admin/users` | `handleListUsers` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users` | `handleCreateUser` |  | admin: admin.users | Super Admin, Admin |

### `public/view-ai-drafts.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/orders/assistant-drafts` | `handleListAssistantDrafts` | ai_access | signed-in: ai_access.default | every signed-in role |
| POST | `/api/v1/orders/{id}/submit-assistant-draft` | `handleSubmitAssistantDraft` | ai_access | signed-in: ai_access.default | every signed-in role |

### `public/view-api-access-detail.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/api-access/attention` | `handleAPIAccessAttention` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}/activity` | `handleAPIAccessActivity` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys/{id}/fix` | `handleAPIAccessFix` |  | admin: api.share | Super Admin, Admin |
| PUT | `/api/v1/api-access/keys/{id}/limits` | `handleAPIAccessSetLimits` |  | admin: api.share | Super Admin, Admin |
| PUT | `/api/v1/api-access/keys/{id}/modules` | `handleAPIAccessUpdateModules` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}/openapi.json` | `handleAPIAccessKeyOpenAPI` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}/postman.json` | `handleAPIAccessKeyPostman` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys/{id}/rotate` | `handleAPIAccessRotateKey` |  | admin: api.share | Super Admin, Admin |
| DELETE | `/api/v1/api-access/keys/{id}/share-docs` | `handleAPIAccessShareDocsWithdraw` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}/share-docs` | `handleAPIAccessShareDocsGet` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys/{id}/share-docs` | `handleAPIAccessShareDocsCreate` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys/{id}/take-over` | `handleAPIAccessTakeOver` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys/{id}/webhooks/deliveries/{d}/resend` | `handleAPIAccessWebhookResend` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}/webhooks/deliveries` | `handleAPIAccessWebhookDeliveries` |  | admin: api.share | Super Admin, Admin |
| DELETE | `/api/v1/api-access/keys/{id}/webhooks/{sub}` | `handleAPIAccessWebhooksDelete` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}/webhooks` | `handleAPIAccessWebhooksList` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys/{id}/webhooks` | `handleAPIAccessWebhooksCreate` |  | admin: api.share | Super Admin, Admin |
| DELETE | `/api/v1/api-access/keys/{id}` | `handleAPIAccessRevokeKey` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys/{id}` | `handleAPIAccessGetKey` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/keys` | `handleAPIAccessListKeys` |  | admin: api.share | Super Admin, Admin |
| POST | `/api/v1/api-access/keys` | `handleAPIAccessCreateKey` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/modules` | `handleAPIAccessModules` |  | admin: api.share | Super Admin, Admin |
| GET | `/api/v1/api-access/openapi.json` | `handleAPIAccessOpenAPI` |  | admin: api.share | Super Admin, Admin |

### `public/view-api-access.js`

Owners: M28. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-asset-extras.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/assets/adjust` | `handleAssetAdjust` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/cwip/capitalize` | `handleAssetCWIPCapitalize` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/cwip/start` | `handleAssetCWIPStart` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/leases/run` | `handleLeaseRunDue` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/leases/{id}/activate` | `handleLeaseActivate` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| GET | `/api/v1/assets/{id}/cwip` | `handleAssetCWIPTotal` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-assets.js`

Screens: Fixed Assets (`#/view/assets`; help: Register and manage fixed assets, verified 2026-09-09).  
Owners: M21–M24. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/assets/capitalize` | `handleCapitalizeAsset` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/depreciation/run` | `handleRunAssetDepreciation` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/dispose` | `handleDisposeAsset` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| GET | `/api/v1/assets/register` | `handleAssetRegister` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| POST | `/api/v1/assets/transfer` | `handleTransferAsset` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| GET | `/api/v1/assets/{id}/disposal-preview` | `handleAssetDisposalPreview` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| GET | `/api/v1/assets/{id}/lifecycle` | `handleAssetLifecycle` | assets | signed-in: assets.manage | Super Admin, Admin, Administrator, Store Supervisor, Accountant |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-attribute-library.js`

Owners: M02, M14. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/platform/tenants/{tenant}/attribute-library` | `handleAttributeLibrary` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/attribute-library` | `handleApplyAttributeLibrary` |  | admin: platform.console | Super Admin |

### `public/view-b2b-pricing.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/orders/{id}/margin-release` | `handleMarginRelease` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/pricing/endings/apply` | `handlePriceEndingsApply` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pricing/endings/preview` | `handlePriceEndingsPreview` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pricing/flags` | `handlePricingFlags` | oms | signed-in: oms.default | every signed-in role |

### `public/view-bank-files.js`

Screens: Bank Files (`#/view/bank-files`; help: Finance & Tax, verified 2026-11-15).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/finance/bank-statement/import` | `handleBankStatementImport` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/payment-proposal/{id}/bank-response` | `handlePaymentBankResponse` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |

### `public/view-bin-tools.js`

Screens: Bin Generator (`#/view/bin-tools`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/wms/bin-generator` | `handleBinGenerator` | wms | signed-in: wms.default | every signed-in role |

### `public/view-branding.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| DELETE | `/api/v1/admin/branding/logo` | `handleBrandingLogoAdmin` |  | admin: admin.settings | Super Admin, Admin, Setup Operator |
| POST | `/api/v1/admin/branding/logo` | `handleBrandingLogoAdmin` |  | admin: admin.settings | Super Admin, Admin, Setup Operator |
| POST | `/api/v1/admin/tenant/feature-flag` | `handleSetFeatureFlag` |  | admin: admin.tenant | Super Admin |
| GET | `/api/v1/branding/logo` | `handleBrandingLogo` |  | public: version.default | anyone |
| GET | `/api/v1/branding` | `handleBranding` |  | public: version.default | anyone |

### `public/view-bulk-purchase.js`

Owners: M03, M04. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/procurement/bulk-upload/apply` | `handleBulkPurchaseApply` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/bulk-upload/preview` | `handleBulkPurchasePreview` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/bulk-upload/runs/{id}/file` | `handleBulkPurchaseRunFile` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/bulk-upload/runs/{id}` | `handleBulkPurchaseRun` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/bulk-upload/runs` | `handleBulkPurchaseRuns` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/bulk-upload/template` | `handleBulkPurchaseTemplate` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/images` | `handleGetPOImages` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/images` | `handleUploadPOImage` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-bulk-sales.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/sales/bulk-upload/apply` | `handleBulkSalesApply` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/sales/bulk-upload/preview` | `handleBulkSalesPreview` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/sales/bulk-upload/runs/{id}/file` | `handleBulkSalesRunFile` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/sales/bulk-upload/runs/{id}` | `handleBulkSalesRun` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/sales/bulk-upload/runs` | `handleBulkSalesRuns` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/sales/bulk-upload/template` | `handleBulkSalesTemplate` |  | signed-in: finance.default | every signed-in role |

### `public/view-channel-listings.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/marketplace/channels/{channel}/listings` | `handleChannelListings` | oms / oms_integration | signed-in: oms.default | every signed-in role |

### `public/view-consumption.js`

Owners: M07, M10, M20. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/inventory/consumption/book` | `handleConsumptionBookStock` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/consumption` | `handlePostConsumptionEntry` | inventory | signed-in: inventory.default | every signed-in role |

### `public/view-control-panel.js`

Screens: Control Panel (`#/view/control-panel`; help: Control Panel and Data Console (platform operators), verified 2026-10-18).  
Owners: M01, M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/platform/control-panel` | `handleControlPanel` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/query-baseline` | `handleControlPanelQueryBaseline` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/attribute-library` | `handleAttributeLibrary` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/attribute-library` | `handleApplyAttributeLibrary` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/open` | `handleControlPanelOpenTenant` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/query-plan` | `handleControlPanelQueryPlan` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/records` | `handlePlatformRecordTypes` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/status` | `handleControlPanelTenantStatus` |  | admin: platform.console | Super Admin |
| GET | `/api/v1/platform/tenants/{tenant}/tables` | `handlePlatformTables` |  | admin: platform.console | Super Admin |
| POST | `/api/v1/platform/tenants/{tenant}/trial` | `handleControlPanelTenantTrial` |  | admin: platform.console | Super Admin |

### `public/view-crm.js`

Screens: CRM (`#/view/crm`; help: CRM & Loyalty, verified 2026-11-15).  
Owners: M19, M24. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/crm/appointments` | `handleCRMAppointments` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/campaigns` | `handleCRMCampaigns` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/conversations` | `handleCRMConversations` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/customers` | `handleCRMCustomers` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/dashboard` | `handleCRMDashboard` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/duplicates` | `handleCRMDuplicates` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/holds` | `handleCRMHolds` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/journeys` | `handleCRMJourneys` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/leads` | `handleCRMLeads` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/lifecycle` | `handleCRMLifecycle` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/lists` | `handleCRMLists` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/loyalty-tier-rules` | `handleLoyaltyTierRules` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/merges` | `handleCRMMerge` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/messages` | `handleCRMMessages` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/meta` | `handleCRMMeta` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/opportunities` | `handleCRMOpportunities` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/readiness` | `handleCRMReadiness` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/referrals` | `handleCRMReferrals` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/segments` | `handleCRMSegments` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/sender-presets` | `handleCRMSenderPresets` | crm_loyalty | admin: crm_loyalty.default | Super Admin, Admin |
| ANY | `/api/v1/crm/senders` | `handleCRMSenders` | crm_loyalty | admin: crm_loyalty.default | Super Admin, Admin |
| ANY | `/api/v1/crm/surveys` | `handleCRMSurveys` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tasks` | `handleCRMTasks` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/templates` | `handleCRMTemplates` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tickets` | `handleCRMTickets` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-custom-orders.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/sales/custom-orders/choices` | `handleCustomOrderChoices` | custom_orders | signed-in: custom_orders.default | every signed-in role |
| POST | `/api/v1/sales/custom-orders/quote` | `handleCustomOrderQuote` | custom_orders | signed-in: custom_orders.default | every signed-in role |
| POST | `/api/v1/sales/custom-orders` | `handleCreateCustomOrder` | custom_orders | signed-in: custom_orders.default | every signed-in role |

### `public/view-dashboards.js`

Owners: M25, M26. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/roles` | `handleListRoles` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles` | `handleCreateRole` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/dashboards/custom/{id}/copy` | `handleCopyCustomDashboard` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/dashboards/custom/{id}/digest` | `handleScheduleDashboardDigest` | reports | signed-in: reports.default | every signed-in role |
| DELETE | `/api/v1/dashboards/custom/{id}` | `handleCustomDashboard` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/dashboards/custom/{id}` | `handleCustomDashboard` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/dashboards/custom` | `handleCustomDashboards` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/dashboards/custom` | `handleCustomDashboards` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/dashboards/datasets` | `handleDashboardDatasets` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/dashboards/export` | `handleDashboardExport` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/dashboards/home` | `handleHomeDashboard` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/dashboards/query` | `handleDashboardQuery` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/me/stores` | `handleMyStores` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/reports/catalog` | `handleReportCatalog` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |

### `public/view-data-governance.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/admin/data-governance/masked-copy` | `handleMaskedCopy` |  | admin: admin.privacy | Super Admin, Admin |
| GET | `/api/v1/admin/data-governance/retention` | `handleListRetentionPolicies` |  | admin: admin.privacy | Super Admin, Admin |
| PUT | `/api/v1/admin/data-governance/retention` | `handleSaveRetentionPolicy` |  | admin: admin.privacy | Super Admin, Admin |

### `public/view-data-healer.js`

Screens: Data Healer (`#/view/datahealer`; help: Data Healer, verified 2026-11-15; Settings and configuration, verified 2026-11-16).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/heals/past-names/apply` | `handlePastRenameApply` |  | admin: master.data-healer | Super Admin, Admin |
| POST | `/api/v1/heals/past-names/preview` | `handlePastRenamePreview` |  | admin: master.data-healer | Super Admin, Admin |
| GET | `/api/v1/heals/{id}/records` | `handleHealRecords` |  | signed-in: master.data-healer | every signed-in role |
| POST | `/api/v1/heals/{id}/run` | `handleRunHeal` |  | signed-in: master.data-healer | every signed-in role |
| GET | `/api/v1/heals` | `handleListHeals` |  | signed-in: master.data-healer | every signed-in role |

### `public/view-data-quality.js`

Screens: Data quality (`#/view/dataquality`; help: Product Information Management, verified 2026-09-03; Settings and configuration, verified 2026-11-16).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/data-quality/scan` | `handleScanDataQuality` |  | signed-in: master.data-quality | every signed-in role |
| GET | `/api/v1/data-quality` | `handleListDataQuality` |  | signed-in: master.data-quality | every signed-in role |

### `public/view-documents.js`

Screens: Records (`#/view/doctype-table`; help: 14 articles, chosen by record type), Doctype Builder (`#/view/doctype-builder`; help: A day as an administrator, verified 2026-08-17).  
Owners: Shared contract across M01–M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/bulk-decide` | `handleBulkDecideApproval` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/expenses/advance/pay` | `handlePayEmployeeAdvance` | expenses | signed-in: expenses.manage | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accountant, HR Manager |
| GET | `/api/v1/meta/doctypes` | `handleGetDocTypes` |  | signed-in: meta.default | every signed-in role |
| POST | `/api/v1/meta/doctypes` | `handleSaveDocType` |  | signed-in: meta.default | every signed-in role |
| DELETE | `/api/v1/meta/{doctype}/fields/{id}` | `handleDeleteFieldDefinition` |  | signed-in: meta.default | every signed-in role |
| PUT | `/api/v1/meta/{doctype}/fields/{id}` | `handleUpdateFieldDefinition` |  | signed-in: meta.default | every signed-in role |
| POST | `/api/v1/meta/{doctype}/fields` | `handleSaveFieldDefinition` |  | signed-in: meta.default | every signed-in role |
| POST | `/api/v1/pim/barcode/generate` | `handlePIMGenerateBarcode` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/bulk-edit` | `handlePIMBulkEdit` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/catalogs/{id}/rotate-share-token` | `handlePIMCatalogRotateShareToken` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/export-templates/{id}/run` | `handlePIMExportTemplateRun` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/product-groups/{id}/export.csv` | `handlePIMProductGroupExport` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/product-groups/{id}/members` | `handlePIMProductGroupMembers` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/taxonomy-history/{doctype}/{id}` | `handlePIMTaxonomyHistory` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/translations/seed` | `handlePIMSeedTranslations` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/procurement/convert-requisition` | `handleConvertRequisition` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/projects/milestone/complete` | `handleCompleteProjectMilestone` | finance | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/projects/revenue/run` | `handleRunProjectRevenue` | finance | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/service/ticket/{id}/assign` | `handleAssignServiceTicket` | service | signed-in: service.default | every signed-in role |
| POST | `/api/v1/service/ticket/{id}/cancel` | `handleCancelServiceTicket` | service | signed-in: service.default | every signed-in role |
| POST | `/api/v1/service/ticket/{id}/close` | `handleCloseServiceTicket` | service | signed-in: service.default | every signed-in role |
| POST | `/api/v1/service/ticket/{id}/resolve` | `handleResolveServiceTicket` | service | signed-in: service.default | every signed-in role |
| POST | `/api/v1/service/ticket/{id}/start` | `handleStartServiceTicket` | service | signed-in: service.default | every signed-in role |

### `public/view-einvoice.js`

Screens: E-Invoice (IRN) (`#/view/einvoice`; help: Finance & Tax, verified 2026-11-15).  
Owners: M17, M18. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/einvoice/badges/{doctype}` | `handleEInvoiceBadges` |  | signed-in | every signed-in role |
| POST | `/api/v1/einvoice/documents/{id}/{action}` | `handleEInvoiceDocumentAction` | finance | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/einvoice/documents/{id}` | `handleGetEInvoiceDocument` | finance | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/einvoice/documents` | `handleListEInvoiceDocuments` | finance | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| POST | `/api/v1/einvoice/profiles/{gstin}/test` | `handleTestEInvoiceProfile` | finance | admin: admin.einvoice | Super Admin, Admin |
| GET | `/api/v1/einvoice/profiles` | `handleListEInvoiceProfiles` | finance | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| POST | `/api/v1/einvoice/profiles` | `handleSaveEInvoiceProfile` | finance | admin: admin.einvoice | Super Admin, Admin |
| POST | `/api/v1/einvoice/run` | `handleRunEInvoiceWorker` | finance | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |

### `public/view-ewaybill.js`

Owners: M17, M18. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/gst/ewaybills/{doctype}/{id}/{action}` | `handleEWBAction` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/gst/ewaybills` | `handleListEWBs` |  | signed-in: doc.default | every signed-in role |

### `public/view-expense-bill.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/procurement/expense-bills/{id}/link-po` | `handleLinkExpenseBill` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/procurement/expense-bills` | `handleListExpenseBills` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/expense-bills` | `handleCreateExpenseBill` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |

### `public/view-expenses.js`

Screens: Expenses (`#/view/expenses`; help: Submit and settle an expense claim, verified 2026-09-09).  
Owners: M21–M24. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/approval/log` | `handleApprovalLog` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| DELETE | `/api/v1/attachments/file/{attachmentID}` | `handleRemoveAttachment` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/attachments/file/{attachmentID}` | `handleDownloadAttachment` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/attachments/{doctype}/{id}` | `handleListAttachments` |  | signed-in: attachments.default | every signed-in role |
| POST | `/api/v1/attachments/{doctype}/{id}` | `handleAddAttachment` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/attachments/{doctype}` | `handleAttachmentCounts` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/expenses/pay` | `handlePayExpenseClaim` | expenses | signed-in: expenses.manage | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accountant, HR Manager |
| POST | `/api/v1/expenses/verify` | `handleVerifyExpenseClaim` | expenses | signed-in: expenses.manage | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accountant, HR Manager |
| GET | `/api/v1/hr/my-employee` | `handleMyEmployeeRecord` |  | signed-in: hr.default | every signed-in role |

### `public/view-finance-coa.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-finance.js`

Screens: Finance (`#/view/finance`; help: Open a shop and make your first sale, verified 2026-11-15; Finance & Tax, verified 2026-11-15; A day in finance, verified 2026-08-17), Vendor Invoices (`#/view/vendor-invoices`; help: Finance & Tax, verified 2026-11-15; Procurement, verified 2026-09-03; A day in finance, verified 2026-08-17), Payment Proposals (`#/view/payment-proposals`; help: Finance & Tax, verified 2026-11-15; A day in finance, verified 2026-08-17), Bank Reconciliation (`#/view/bank-reconciliation`; help: Finance & Tax, verified 2026-11-15; A day in finance, verified 2026-08-17), Finance Notes (`#/view/finance-notes`; help: Finance & Tax, verified 2026-11-15; A day in finance, verified 2026-08-17), Sales Invoices (`#/view/sales-invoices`; help: Your first order, end to end, verified 2026-11-14; Finance & Tax, verified 2026-11-15; A day in finance, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17).  
Owners: M17, M18. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/finance/acquirer-settlements/match` | `handleMatchAcquirerSettlements` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/bank-reconcile` | `handleBankReconcile` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/bank-reconciliation/lines/{id}/candidates` | `handleBankLineCandidates` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/bank-reconciliation/lines/{id}/{action}` | `handleBankLineAction` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/bank-reconciliation/rule-match` | `handleBankRuleMatch` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/bank-reconciliation/workbench` | `handleBankReconWorkbench` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/credit-note/{id}/post` | `handlePostCreditNote` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/debit-note/{id}/post` | `handlePostDebitNote` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/payment-proposal/{id}/execute` | `handleExecutePaymentProposal` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/payment-proposal` | `handlePaymentProposal` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/period-reopen/{request}/apply` | `handlePeriodReopenApply` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/periods/{id}/close-board` | `handlePeriodCloseBoard` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/finance/periods/{id}/close-checklist` | `handlePeriodCloseChecklist` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/periods/{id}/close-tasks/{task}/{action}` | `handlePeriodCloseTaskAction` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/periods/{id}/close` | `handleCloseAccountingPeriod` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/periods/{id}/hold` | `handleHoldAccountingPeriod` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/periods/{id}/release` | `handleReleaseAccountingPeriod` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/periods/{id}/reopen-request` | `handlePeriodReopenRequest` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/periods` | `handleAccountingPeriods` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/periods` | `handleAccountingPeriods` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/sales-invoice/{id}/cancel` | `handleCancelSalesInvoice` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/sales-invoice/{id}/payment-link` | `handleSalesInvoicePaymentLink` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/sales-invoice/{id}/post` | `handlePostSalesInvoice` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/sales-invoice/{id}/settle` | `handleSettleSalesInvoice` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/set-off/suggestions` | `handleSetOffSuggestions` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/finance/trial-balance` | `handleTrialBalance` |  | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| POST | `/api/v1/finance/vendor-credit-note/{id}/post` | `handlePostVendorCreditNote` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/procurement/expense-bills` | `handleListExpenseBills` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/expense-bills` | `handleCreateExpenseBill` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-invoice/match` | `handleMatchVendorInvoice` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/vendor-invoice/pay-with-tds` | `handlePayVendorInvoiceWithTDS` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-invoice/pay` | `handlePayVendorInvoice` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |

### `public/view-gate-entry.js`

Screens: Gate entries (`#/view/gate-entry`; help: Procurement, verified 2026-09-03).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/attachments/{doctype}/{id}` | `handleListAttachments` |  | signed-in: attachments.default | every signed-in role |
| POST | `/api/v1/attachments/{doctype}/{id}` | `handleAddAttachment` |  | signed-in: attachments.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-golive.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/golive/graph/scope` | `handleGoLiveGraphScope` |  | admin: admin.golive | Super Admin, Admin |
| GET | `/api/v1/golive/graph` | `handleGoLiveGraph` |  | admin: admin.golive | Super Admin, Admin |
| POST | `/api/v1/golive/packs/{pack}/evidence` | `handleGoLiveEvidence` |  | admin: admin.golive | Super Admin, Admin |
| POST | `/api/v1/golive/packs/{pack}/signoff` | `handleGoLiveSignOff` |  | admin: admin.golive | Super Admin, Admin |
| POST | `/api/v1/golive/packs/{pack}/waive/end` | `handleGoLiveWaiveEnd` |  | admin: admin.golive | Super Admin, Admin |
| POST | `/api/v1/golive/packs/{pack}/waive` | `handleGoLiveWaive` |  | admin: admin.golive | Super Admin, Admin |
| GET | `/api/v1/golive/packs/{pack}` | `handleGoLivePack` |  | admin: admin.golive | Super Admin, Admin |
| GET | `/api/v1/golive/scope` | `handleGoLiveScopeGet` |  | admin: admin.golive | Super Admin, Admin |
| POST | `/api/v1/golive/scope` | `handleGoLiveScopeSave` |  | admin: admin.golive | Super Admin, Admin |

### `public/view-gstr-files.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/gstr2b/import` | `handleGSTR2BImport` | reports | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/gstr2b/lines/{id}/decision` | `handleGSTR2BDecision` | reports | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/gstr2b/reconcile` | `handleGSTR2BReconcile` | reports | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/reports/gstr1-json` | `handleGSTR1JSON` | reports | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |

### `public/view-gstr1-lock.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/finance/gstr1-filed/{month}/unmark` | `handleGSTR1Unmark` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/gstr1-filed/{month}` | `handleGSTR1MarkFiled` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/gstr1-filed` | `handleGSTR1FiledMonths` |  | signed-in: finance.default | every signed-in role |

### `public/view-handoff.js`

Owners: M12, M13, M28. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/auth/operator-handoff` | `handleOperatorHandoff` |  | public: auth.default | anyone |

### `public/view-help.js`

Screens: Help (`#/view/help`; help: Find help for your task, verified 2026-11-15).  
Owners: Shared contract across M01–M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/help/article/{slug}` | `handleHelpArticle` |  | signed-in: help.default | every signed-in role |
| GET | `/api/v1/help/img/{name}` | `handleHelpSnapshot` |  | signed-in: help.default | every signed-in role |
| GET | `/api/v1/help/index` | `handleHelpIndex` |  | signed-in: help.default | every signed-in role |
| GET | `/api/v1/help/search-index` | `handleHelpSearchIndex` |  | signed-in: help.default | every signed-in role |

### `public/view-hr-pay-extras.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/hr/appraisal-cycles/{id}/rating-spread` | `handleAppraisalRatingSpread` | hr | signed-in: hr.default | every signed-in role |
| POST | `/api/v1/hr/appraisals/{id}/calibrate` | `handleCalibrateAppraisal` | hr | signed-in: hr.default | every signed-in role |
| GET | `/api/v1/hr/form-24q` | `handleForm24Q` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |
| GET | `/api/v1/hr/pf-ecr` | `handlePFECR` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |
| GET | `/api/v1/hr/salary-revisions/{id}/arrears` | `handleSalaryRevisionArrears` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |

### `public/view-hr-people.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/hr/certificates-expiring` | `handleExpiringCertificates` | hr | signed-in: hr.default | every signed-in role |
| POST | `/api/v1/hr/job-offers/{id}/accept` | `handleAcceptJobOffer` | hr | signed-in: hr.default | every signed-in role |
| POST | `/api/v1/hr/onboarding/{id}/create-login` | `handleCreateLoginFromOnboarding` | hr | admin: hr.default | Super Admin, Admin |
| POST | `/api/v1/hr/onboarding/{id}/steps` | `handleOnboardingStep` | hr | signed-in: hr.default | every signed-in role |
| GET | `/api/v1/hr/people-analytics` | `handlePeopleAnalytics` | hr | signed-in: hr.default | every signed-in role |

### `public/view-hr.js`

Screens: HR (`#/view/hr`; help: HR & Payroll, verified 2026-09-03).  
Owners: M21–M24. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/decide` | `handleDecideApproval` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/approval/log` | `handleApprovalLog` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/approval/rules` | `handleApprovalRules` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/hr/disburse-loan` | `handleDisburseEmployeeLoan` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |
| GET | `/api/v1/hr/grievance-access` | `handleGrievanceAccess` | hr | signed-in: hr.default | every signed-in role |
| GET | `/api/v1/hr/leave-balance` | `handleLeaveBalance` | hr | signed-in: hr.default | every signed-in role |
| GET | `/api/v1/hr/my-employee` | `handleMyEmployeeRecord` |  | signed-in: hr.default | every signed-in role |
| POST | `/api/v1/hr/onboarding/{id}/steps` | `handleOnboardingStep` | hr | signed-in: hr.default | every signed-in role |
| GET | `/api/v1/hr/payroll-export` | `handlePayrollExport` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |
| POST | `/api/v1/hr/post-payslip` | `handlePostPayslip` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |
| POST | `/api/v1/hr/run-payroll` | `handleRunPayroll` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |

### `public/view-images.js`

Owners: M14, M27. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/image-drive/copy` | `handleImageDriveCopyPreview` |  | admin: admin.image_drive | Super Admin, Admin |
| POST | `/api/v1/admin/image-drive/copy` | `handleImageDriveCopyStart` |  | admin: admin.image_drive | Super Admin, Admin |
| POST | `/api/v1/admin/image-drive/test` | `handleTestImageDrive` |  | admin: admin.image_drive | Super Admin, Admin |
| GET | `/api/v1/admin/image-drive` | `handleGetImageDrive` |  | admin: admin.image_drive | Super Admin, Admin |
| POST | `/api/v1/admin/image-drive` | `handleSaveImageDrive` |  | admin: admin.image_drive | Super Admin, Admin |
| POST | `/api/v1/images/main` | `handleOwnerImageAction` |  | signed-in: images.default | every signed-in role |
| POST | `/api/v1/images/remove` | `handleOwnerImageAction` |  | signed-in: images.default | every signed-in role |
| GET | `/api/v1/images/{id}/file` | `handleImageFile` |  | signed-in: images.default | every signed-in role |
| GET | `/api/v1/images/{id}/thumbnail` | `handleImageThumbnail` |  | signed-in: images.default | every signed-in role |
| GET | `/api/v1/images` | `handleListOwnerImages` |  | signed-in: images.default | every signed-in role |
| POST | `/api/v1/images` | `handleUploadOwnerImage` |  | signed-in: images.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/images` | `handleGetPOImages` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/images` | `handleUploadPOImage` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-import-jobs.js`

Owners: M28. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/import-jobs/{id}/failed-rows.csv` | `handleImportJobFailedRows` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-jobs/{id}/resume` | `handleResumeImportJob` |  | signed-in: import.default | every signed-in role |
| GET | `/api/v1/import-jobs/{id}` | `handleImportJobSummary` |  | signed-in: import.default | every signed-in role |

### `public/view-import-modal.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/import/{doctype}/template` | `handleGetImportTemplate` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import/{doctype}` | `handleBulkImport` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/pos/offers/item-lists` | `handleOfferItemListImport` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/offers/preview` | `handlePOSOffersPreview` |  | signed-in: pos.default | every signed-in role |

### `public/view-import-staging.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/availability` | `handleGetAvailability` |  | signed-in: availability.default | every signed-in role |
| GET | `/api/v1/branding` | `handleBranding` |  | public: version.default | anyone |
| POST | `/api/v1/checkout` | `handleCheckout` |  | signed-in: checkout.default | every signed-in role |
| GET | `/api/v1/data-quality` | `handleListDataQuality` |  | signed-in: master.data-quality | every signed-in role |
| GET | `/api/v1/doc-find` | `handleDocFind` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/grn-steps` | `handleGRNStepsInbox` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/heals` | `handleListHeals` |  | signed-in: master.data-healer | every signed-in role |
| GET | `/api/v1/health` | `handleHealth` |  | public: health.default | anyone |
| GET | `/api/v1/images` | `handleListOwnerImages` |  | signed-in: images.default | every signed-in role |
| POST | `/api/v1/images` | `handleUploadOwnerImage` |  | signed-in: images.default | every signed-in role |
| GET | `/api/v1/import-batches` | `handleImportBatchList` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches` | `handleImportBatchCreate` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import/{doctype}` | `handleBulkImport` |  | signed-in: import.default | every signed-in role |
| GET | `/api/v1/inbox` | `handleTaskInbox` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/jobs` | `handleListJobs` |  | signed-in: jobs.view | Super Admin, Admin, Administrator, Auditor |
| ANY | `/api/v1/labels` | `handleLabels` |  | signed-in: labels.default | every signed-in role |
| GET | `/api/v1/localization` | `handleGetLocalization` |  | signed-in: localization.default | every signed-in role |
| POST | `/api/v1/login` | `handleLogin` |  | public: login.default | anyone |
| GET | `/api/v1/me` | `handleGetProfile` |  | signed-in: me.default | every signed-in role |
| PUT | `/api/v1/me` | `handleUpdateProfile` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/orders` | `handleCreateSalesOrder` | oms | signed-in: oms.default | every signed-in role |
| ANY | `/api/v1/prefix` | `handlePrefix` |  | signed-in: prefix.default | every signed-in role |
| GET | `/api/v1/ready` | `handleReadiness` |  | public: health.default | anyone |
| GET | `/api/v1/reason-codes` | `handleListReasonCodes` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/reserve` | `handleCreateReservation` |  | signed-in: reserve.default | every signed-in role |
| POST | `/api/v1/returns` | `handleCreateReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| ANY | `/api/v1/sequence` | `handleSequence` |  | signed-in: sequence.default | every signed-in role |
| GET | `/api/v1/stock-age-inspections` | `handleStockAgeInspections` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/stock-age-inspections` | `handleStockAgeInspections` | quality | signed-in: quality.default | every signed-in role |
| GET | `/api/v1/stock-locations` | `handleStockLocations` |  | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/stock-locations` | `handleStockLocations` |  | admin: admin.stock_locations | Super Admin, Admin |
| GET | `/api/v1/version` | `handleVersion` |  | public: version.default | anyone |

### `public/view-inbox.js`

Screens: Approvals (`#/view/approvals`; help: Open a shop and make your first sale, verified 2026-11-15; Finance & Tax, verified 2026-11-15; Point of Sale, verified 2026-11-16; Procurement, verified 2026-09-03; Security, Roles & Approvals, verified 2026-11-15; A day in finance, verified 2026-08-17; A day as a store manager, verified 2026-08-17).  
Owners: Shared contract across M01–M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/inbox` | `handleTaskInbox` |  | signed-in: approval.default | every signed-in role |

### `public/view-integration-credentials.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/clevertap/credentials` | `handleGetCleverTapCredentials` |  | signed-in: clevertap.default | every signed-in role |
| POST | `/api/v1/clevertap/credentials` | `handleCleverTapCredentials` |  | signed-in: clevertap.default | every signed-in role |
| POST | `/api/v1/integrations/credentials/active` | `handleSetConnectorCredentialActive` |  | admin: admin.integrations | Super Admin, Admin |
| POST | `/api/v1/integrations/credentials/disconnect` | `handleConnectorDisconnect` |  | admin: admin.integrations | Super Admin, Admin |
| POST | `/api/v1/integrations/credentials/rotate` | `handleRotateConnectorCredential` |  | admin: admin.integrations | Super Admin, Admin |
| GET | `/api/v1/integrations/credentials` | `handleListConnectorCredentials` |  | admin: admin.integrations | Super Admin, Admin |
| GET | `/api/v1/marketplace/channels/{channel}/credentials` | `handleChannelConnectorCredentials` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/credentials` | `handleChannelConnectorCredentials` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/couriers/{provider}/credentials` | `handleCourierCredentials` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/couriers/{provider}/credentials` | `handleCourierCredentials` | oms / oms_integration | signed-in: oms.default | every signed-in role |

### `public/view-inventory-erp.js`

Owners: M07, M10, M20. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/adjustments/preview` | `handlePreviewInventoryAdjustment` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/adjustments/{id}/post` | `handlePostInventoryAdjustment` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/adjustments/{id}/reverse` | `handleReverseInventoryAdjustment` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/adjustments` | `handleCreateInventoryAdjustment` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/challans/{id}/cancel` | `handleCancelDeliveryChallan` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/challans/{id}/dispatch` | `handleDispatchDeliveryChallan` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/challans/{id}/return` | `handleReturnDeliveryChallan` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/challans/{id}/write-off` | `handleRequestChallanWriteOff` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/challans` | `handleCreateDeliveryChallan` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/dashboard` | `handleInventoryDashboard` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/material-requests/{id}/issue` | `handleIssueMaterial` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/material-requests/{id}/return` | `handleReturnMaterial` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/material-requests` | `handleCreateMaterialRequest` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/mrp-changes/{id}/cancel` | `handleCancelMRPChange` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/mrp-changes` | `handleCreateMRPChange` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |

### `public/view-item-form.js`

Owners: M02, M14. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/adjustments` | `handleCreateInventoryAdjustment` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/attribute-map/suggest` | `handleSuggestAttributeMap` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/attribute-value` | `handleAddItemAttributeValue` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/challans` | `handleCreateDeliveryChallan` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/consumption` | `handlePostConsumptionEntry` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/dashboard` | `handleInventoryDashboard` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/item-attributes` | `handleGetItemAttributes` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/item-setup/fields/list` | `handleCreateItemFieldList` |  | signed-in: doc.default | every signed-in role |
| PUT | `/api/v1/inventory/item-setup/fields` | `handleSaveItemFieldRules` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/inventory/item-setup` | `handleGetItemSetup` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/material-requests` | `handleCreateMaterialRequest` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/mrp-changes` | `handleCreateMRPChange` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/piece-holds` | `handleListPieceHolds` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/piece-holds` | `handlePlacePieceHold` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/stock` | `handleInventoryStock` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/treatment-correction/apply` | `handleTreatmentCorrectionApply` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/inventory/treatment-correction/preview` | `handleTreatmentCorrectionPreview` |  | signed-in: doc.default | every signed-in role |

### `public/view-jobwork.js`

Owners: M15, M16. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/manufacturing/subcontract-order/charge` | `handleJobWorkCharge` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/subcontract-order/receive-lines` | `handleReceiveJobWorkLines` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/subcontract-order/receive` | `handleReceiveSubcontractOrder` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/subcontract-order/send` | `handleSendSubcontractOrder` | manufacturing | signed-in: manufacturing.default | every signed-in role |

### `public/view-journal-tools.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/finance/fx-revaluation` | `handleFXRevaluation` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/fx-revaluation` | `handleFXRevaluation` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/intercompany-transaction` | `handleCreateIntercompanyTransaction` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |

### `public/view-journal.js`

Screens: Journal Vouchers (`#/view/journal-vouchers`; help: Finance & Tax, verified 2026-11-15).  
Owners: M17, M18. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/finance/journal-voucher/{id}/retry-post` | `handleRetryPostJournalVoucher` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/journal-voucher/{id}/reverse` | `handleReverseJournalVoucher` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/journal-voucher` | `handleCreateJournalVoucher` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/trial-balance` | `handleTrialBalance` |  | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |

### `public/view-loyalty.js`

Screens: Loyalty (`#/view/loyalty`; help: CRM & Loyalty, verified 2026-11-15).  
Owners: M19, M24. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/crm/loyalty-tier-rules` | `handleLoyaltyTierRules` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/loyalty/adjustments` | `handleLoyaltyAdjustments` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/loyalty/campaigns` | `handleLoyaltyCampaignOverview` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/loyalty/ledger` | `handleLoyaltyLedger` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/loyalty/transfers/{id}/confirm` | `handleLoyaltyTransferConfirm` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/loyalty/transfers` | `handleLoyaltyTransfers` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/loyalty/vouchers` | `handleLoyaltyVoucherOverview` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/pos/customer-context` | `handlePOSCustomerContext` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |

### `public/view-manufacturing-confirmations.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/manufacturing/confirmations` | `handleListProductionConfirmations` | manufacturing | signed-in: manufacturing.default | every signed-in role |

### `public/view-manufacturing.js`

Screens: Manufacturing (`#/view/manufacturing`; help: Manufacturing & MRP, verified 2026-11-15).  
Owners: M15, M16. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/manufacturing/acknowledge-bom-variance` | `handleAcknowledgeBOMVariance` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/complete` | `handleCompleteProductionOrder` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/confirm-operation` | `handleConfirmProductionOperation` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/issue-material` | `handleIssueProductionMaterial` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| GET | `/api/v1/manufacturing/material-plan` | `handleProductionMaterialPlan` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| GET | `/api/v1/manufacturing/mrp-suggestions` | `handleMRPSuggestions` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/partial-complete` | `handlePartialCompleteProductionOrder` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| GET | `/api/v1/manufacturing/production-schedule` | `handleGetProductionSchedule` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/record-actual-cost` | `handleRecordActualProductionCost` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/rework` | `handleSendProductionToRework` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/scrap` | `handlePostProductionScrap` | manufacturing | signed-in: manufacturing.default | every signed-in role |

### `public/view-market-harvest.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/pim/market-harvest` | `handlePIMMarketHarvest` | pim | signed-in: pim.default | every signed-in role |

### `public/view-master-recode.js`

Owners: M02, M14. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/master-codes/Location/code` | `handleLocationCodeChange` |  | admin: admin.master-codes | Super Admin, Admin |
| POST | `/api/v1/master-codes/Location/code` | `handleLocationCodeChange` |  | admin: admin.master-codes | Super Admin, Admin |
| GET | `/api/v1/master-codes/{doctype}/clean` | `handleMasterCodeClean` |  | admin: admin.master-codes | Super Admin, Admin |
| POST | `/api/v1/master-codes/{doctype}/clean` | `handleMasterCodeClean` |  | admin: admin.master-codes | Super Admin, Admin |

### `public/view-offline-sales.js`

Screens: Offline Sales (`#/view/offline-sales`; help: Point of Sale, verified 2026-11-16).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/pos/offline-sales/{id}/reject` | `handleRejectOfflineSale` |  | signed-in: pos.offline_validate | Super Admin, Admin, Administrator, Store Supervisor |
| POST | `/api/v1/pos/offline-sales/{id}/validate` | `handleValidateOfflineSale` |  | signed-in: pos.offline_validate | Super Admin, Admin, Administrator, Store Supervisor |
| GET | `/api/v1/pos/offline-sales/{id}` | `handleGetOfflineSale` |  | signed-in: pos.offline_validate | Super Admin, Admin, Administrator, Store Supervisor |
| GET | `/api/v1/pos/offline-sales` | `handleListOfflineSales` |  | signed-in: pos.offline_validate | Super Admin, Admin, Administrator, Store Supervisor |

### `public/view-oms-freight.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/oms/carrier-claims/{id}/move` | `handleMoveCarrierClaim` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/carrier-claims` | `handleRaiseCarrierClaim` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/freight/reconcile` | `handleReconcileFreightInvoices` | oms | signed-in: oms.default | every signed-in role |

### `public/view-oms-steps.js`

Owners: M12, M13, M28. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/fulfillment/task/transition` | `handleFulfillmentTaskTransition` | wms_integration | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/marketplace/couriers/rates` | `handleCourierRates` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/couriers/{provider}/resolve` | `handleCourierResolveBooking` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/logistics/book` | `handleLogisticsBook` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/logistics/manifest/handover` | `handleHandoverManifest` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/logistics/manifest` | `handleGenerateManifest` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/logistics/rto` | `handleShipmentRTO` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/logistics/tracking` | `handleShipmentTracking` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/backorders/release` | `handleReleaseBackorders` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/gate-passes/{id}/complete` | `handleGatePassTransition` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/gate-passes/{id}/issue` | `handleGatePassTransition` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/gate-passes` | `handleSearchGatePasses` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/gate-passes` | `handleCreateGatePass` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/orders/bulk` | `handleOMSBulkAction` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/orders/search` | `handleOMSOrderSearch` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/orders/{id}` | `handleOMSOrderDetail` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/orders` | `handleOMSOrderList` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/pick-queue` | `handlePickQueue` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/shipping-packages/{id}/invoice` | `handleInvoiceShippingPackage` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/shipping-packages/{id}/update` | `handleUpdateShippingPackage` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/shipping-packages` | `handleListShippingPackages` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/shipping-packages` | `handleCreateShippingPackage` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/release-hold` | `handleReleaseOrderHold` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/wms/bin-contents` | `handleBinContents` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/bin-generator` | `handleBinGenerator` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/bin-move` | `handleBinMove` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/cockpit` | `handleWarehouseCockpit` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/condition-transition` | `handleBinConditionTransition` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/inbound-work` | `handleInboundWork` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/order-owner` | `handleSetOrderOwner` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/outbound-work` | `handleOutboundWork` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/owner-availability` | `handleOwnerAvailability` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/pack-complete` | `handlePackCompleteValidated` |  | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/pack-scan` | `handlePackScan` |  | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/packing-validation` | `handlePackingValidation` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/pick-list` | `handlePickList` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/pick-scan` | `handlePickScan` |  | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/piece-codes` | `handlePieceCodeMint` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/putaway` | `handlePutaway` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/short-pick` | `handleShortPick` |  | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/work-queue` | `handleWorkQueue` | wms | signed-in: wms.default | every signed-in role |

### `public/view-oms.js`

Screens: OMS (`#/view/oms`; help: Your first order, end to end, verified 2026-11-14; Channel Connectors, verified 2026-09-03; Courier Integrations, verified 2026-09-03; Order Management, verified 2026-09-03; Shopify, verified 2026-10-22; A day as a store manager, verified 2026-08-17; A day as a warehouse operator, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17).  
Owners: M12, M13, M28. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/availability` | `handleGetAvailability` |  | signed-in: availability.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/marketplace/channels/{channel}/credentials` | `handleChannelConnectorCredentials` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/credentials` | `handleChannelConnectorCredentials` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/channels/{channel}/listings` | `handleChannelListings` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/pull-orders` | `handleChannelOrderPull` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/push-status` | `handleChannelStatusPush` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/channels/{channel}/setup-check` | `handleChannelSetupCheck` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/sync-inventory` | `handleChannelInventorySync` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/connectors/health` | `handleConnectorHealth` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/logistics/label` | `handleShippingLabel` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/settlement/reconcile` | `handleMarketplaceReconcile` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/sku-exceptions` | `handleChannelSKUExceptions` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/sku-mappings` | `handleChannelSKUMappings` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/sku-mappings` | `handleChannelSKUMappings` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/bundles/{sku}/availability` | `handleBundleAvailability` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/orders/bulk` | `handleOMSBulkAction` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/orders/search` | `handleOMSOrderSearch` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/orders/{id}/channel-change-done` | `handleChannelChangeDone` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/orders/{id}` | `handleOMSOrderDetail` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/orders` | `handleOMSOrderList` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/promise` | `handlePromisePreview` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/queues/{queue}` | `handleOMSExceptionQueues` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/queues` | `handleOMSExceptionQueues` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/settlements/reconcile` | `handleReconcileMarketplaceSettlements` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/settlements/{id}/dispute` | `handleRaiseSettlementDispute` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/settlements/{id}/resolve` | `handleResolveSettlementDispute` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/settlements/{id}/write-off` | `handleWriteOffSettlementVariance` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/tiles` | `handleOMSConsoleTiles` | oms | signed-in: oms.default | every signed-in role |
| DELETE | `/api/v1/oms/views/{id}` | `handleOMSDeleteSavedView` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/oms/views` | `handleOMSSavedViews` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/views` | `handleOMSSavedViews` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/order-lines/{lineId}/hold` | `handleHoldOrderLine` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/order-lines/{lineId}/release-hold` | `handleReleaseOrderLineHold` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/orders/assistant-drafts` | `handleListAssistantDrafts` | ai_access | signed-in: ai_access.default | every signed-in role |
| POST | `/api/v1/orders/{id}/cancel` | `handleCancelOrder` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/credit-notes` | `handleOrderCreditNotes` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/edit` | `handleEditOrder` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/lines/{lineId}/substitute` | `handleSubstituteOrderLine` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/lines` | `handleEditOrderLines` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/release-hold` | `handleReleaseOrderHold` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/replacement` | `handleCreateReplacementOrder` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders` | `handleCreateSalesOrder` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/pricing/line` | `handlePricingLine` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/returns` | `handleCreateReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/wms/bundles/{operation}` | `handleBundleAssembly` | wms | signed-in: wms.default | every signed-in role |

### `public/view-pickers.js`

Owners: M02, M14. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/hsn/suggest` | `handleHSNSuggest` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/lines/resolve` | `handleResolveLineCodes` |  | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/master-codes/Location/code` | `handleLocationCodeChange` |  | admin: admin.master-codes | Super Admin, Admin |
| POST | `/api/v1/master-codes/Location/code` | `handleLocationCodeChange` |  | admin: admin.master-codes | Super Admin, Admin |
| GET | `/api/v1/master-codes/{doctype}` | `handleMasterCodeSeries` |  | signed-in: meta.default | every signed-in role |
| POST | `/api/v1/pim/barcode/generate` | `handlePIMGenerateBarcode` | pim | signed-in: pim.default | every signed-in role |

### `public/view-piece-correction.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/inventory/pieces/{doctype}/{id}` | `handleGetPieceAttributes` | inventory | signed-in: inventory.default | every signed-in role |

### `public/view-piece-hold.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/inventory/piece-holds/release` | `handleReleasePieceHold` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/inventory/piece-holds` | `handleListPieceHolds` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/inventory/piece-holds` | `handlePlacePieceHold` | inventory | signed-in: inventory.default | every signed-in role |

### `public/view-pim-category-rules.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/pim/category-rules` | `handlePIMCategoryRules` | pim | signed-in: pim.default | every signed-in role |

### `public/view-pim-data.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/pim/channel-templates/load` | `handlePIMChannelTemplateLoad` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/channel-templates/{id}/coverage` | `handlePIMChannelTemplateCoverage` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/checklists/{id}` | `handlePIMChecklist` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/checklists` | `handlePIMChecklists` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/checklists` | `handlePIMChecklists` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/design-tags` | `handlePIMDesignTags` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/design-values` | `handlePIMDesignValues` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/design-values` | `handlePIMDesignValues` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/feeds/{id}/run` | `handlePIMFeedRun` | pim | signed-in: pim.default | every signed-in role |

### `public/view-pim-extras.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/pim/items/merge` | `handlePIMItemMerge` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/live-on/{itemCode}` | `handlePIMLiveOn` | pim | signed-in: pim.default | every signed-in role |

### `public/view-pim-range.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/listings/pull` | `handleChannelListingsPull` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/pim/assortment/bulk` | `handleAssortmentBulk` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/listing-drift` | `handleListingDrift` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/markdown-calendar` | `handleMarkdownCalendar` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/publish` | `handlePIMPublish` | pim | signed-in: pim.default | every signed-in role |

### `public/view-pim-stewardship.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/pim/media-order` | `handlePIMMediaOrder` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/media-order` | `handlePIMMediaOrder` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/stewardship/{attribute}` | `handlePIMStewardshipDetail` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/stewardship` | `handlePIMStewardship` | pim | signed-in: pim.default | every signed-in role |

### `public/view-pim.js`

Screens: PIM (`#/view/pim`; help: Channel Connectors, verified 2026-09-03; Product Groups, verified 2026-11-15; Product Information Management, verified 2026-09-03; A day as a category manager, verified 2026-08-17).  
Owners: M14, M27. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/approval/log` | `handleApprovalLog` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/pim/assignable-users` | `handlePIMAssignableUsers` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/bulk-edit` | `handlePIMBulkEdit` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/catalog-share` | `handlePIMCatalogShare` | pim | public: pim.default | anyone |
| GET | `/api/v1/pim/category-rules` | `handlePIMCategoryRules` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/channels/{code}/apply-pack` | `handlePIMApplyChannelPack` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/checklists` | `handlePIMChecklists` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/checklists` | `handlePIMChecklists` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/completeness/{itemCode}` | `handlePIMCompleteness` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/content-assist-shapes` | `handlePIMContentAssistShapes` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/content-assist/{itemCode}` | `handlePIMContentAssist` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/content/{id}/rollback` | `handlePIMContentRollback` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/content/{id}/versions` | `handlePIMContentVersions` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/dashboard` | `handlePIMDashboard` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/design-tags` | `handlePIMDesignTags` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/design-values` | `handlePIMDesignValues` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/design-values` | `handlePIMDesignValues` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/export-templates/{id}/run` | `handlePIMExportTemplateRun` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/import-jobs/{id}/errors.csv` | `handlePIMImportJobErrors` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/import-schedules` | `handlePIMImportSchedules` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/import-templates/{id}/import` | `handlePIMImportTemplateRun` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/import-templates/{id}/preview-mapping` | `handlePIMImportTemplatePreviewMapping` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/import-templates/{id}/preview` | `handlePIMImportTemplateRun` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/import-templates` | `handlePIMImportTemplates` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/listing-drift` | `handleListingDrift` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/markdown-calendar` | `handleMarkdownCalendar` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/market-harvest` | `handlePIMMarketHarvest` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media-order` | `handlePIMMediaOrder` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/media-order` | `handlePIMMediaOrder` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media/bulk-download` | `handlePIMMediaBulkDownload` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/media/bulk-upload` | `handlePIMMediaBulkUpload` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media/search` | `handlePIMMediaSearch` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/media/upload` | `handlePIMMediaUpload` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/media/{id}/deactivate` | `handlePIMMediaDeactivate` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media/{id}/file` | `handlePIMMediaFile` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/media/{id}/metadata` | `handlePIMMediaUpdateMetadata` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media/{id}/thumbnail` | `handlePIMMediaThumbnail` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media/{id}/transform/{preset}` | `handlePIMMediaTransform` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media` | `handlePIMMediaList` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/publish-log` | `handlePIMPublishLog` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/publish-preview` | `handlePIMPublishPreview` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/publish` | `handlePIMPublish` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/related-products/{itemCode}` | `handlePIMRelatedProducts` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/reports/{name}` | `handlePIMReport` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/search-feed.csv` | `handlePIMSearchFeedExport` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/stewardship` | `handlePIMStewardship` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/task-templates/{code}/instantiate` | `handlePIMTaskTemplateInstantiate` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/task-templates` | `handlePIMTaskTemplates` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/tasks/bulk` | `handlePIMTaskBulk` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/tasks/{id}/{action}` | `handlePIMTaskAction` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/tasks` | `handlePIMTasks` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/tasks` | `handlePIMTasks` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/transform-rules` | `handlePIMTransformRules` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/workbench` | `handlePIMWorkbench` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/workflow-runs/{id}/action` | `handlePIMWorkflowRunAction` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/workflow-runs` | `handlePIMWorkflowRuns` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/workflows/{code}/start` | `handlePIMWorkflowStart` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/workflows` | `handlePIMWorkflows` | pim | signed-in: pim.default | every signed-in role |

### `public/view-planning.js`

Owners: M25, M26. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/planning/proposals/approve` | `handleApproveSupplyProposals` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/planning/proposals/generate` | `handleGenerateSupplyProposals` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/planning/proposals/reject` | `handleRejectSupplyProposals` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/planning/proposals` | `handleListSupplyProposals` | inventory | signed-in: inventory.default | every signed-in role |

### `public/view-pos-customer.js`

Owners: M05, M06, M19. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/pos/advances/refund` | `handlePOSAdvanceRefund` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/advances` | `handlePOSAdvances` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/advances` | `handlePOSAdvances` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/customer-lookup` | `handlePOSCustomerLookup` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/high-value-policy` | `handlePOSHighValuePolicy` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/salesmen` | `handlePOSSalesmen` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/tenders` | `handlePOSTenders` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/walk-in-customer` | `handlePOSWalkInCustomer` |  | signed-in: pos.default | every signed-in role |

### `public/view-pos-extras.js`

Owners: M05, M06, M19. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/availability` | `handleGetAvailability` |  | signed-in: availability.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/pos/bills/{id}/cancel` | `handleCancelPOSBill` | oms | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/bills` | `handlePOSBills` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/customer-context` | `handlePOSCustomerContext` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/exchange/start` | `handleStartPOSExchange` | oms | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/gift-card/balance` | `handleGiftCardBalance` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/gift-card/issue` | `handleIssueGiftCard` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/held-bills/{id}/{action}` | `handleHeldPOSBillAction` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/held-bills` | `handleListHeldPOSBills` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/held-bills` | `handleHoldPOSBill` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/loyalty/redemption/start` | `handleStartPOSRedemption` | crm_loyalty | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/loyalty/redemption/verify` | `handleVerifyPOSRedemption` | crm_loyalty | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/price-override` | `handlePOSPriceOverride` |  | signed-in: pos.price_override | Super Admin, Admin, Administrator, Store Supervisor |
| POST | `/api/v1/pos/session/cash-movement` | `handlePOSCashMovement` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/session/close` | `handlePOSSessionClose` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/session/summary` | `handlePOSSessionSummary` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/session/takeover` | `handlePOSSessionTakeover` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/returns/eligibility` | `handleReturnEligibility` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/returns` | `handleCreateReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/store-app/devices` | `handleStoreAppDevices` |  | signed-in: store_app.devices | every signed-in role |
| POST | `/api/v1/store-app/devices` | `handleStoreAppDevices` |  | signed-in: store_app.devices | every signed-in role |
| GET | `/api/v1/store-app/item` | `handleStoreAppItem` |  | signed-in: availability.default | every signed-in role |
| POST | `/api/v1/store-app/pin/login` | `handleStoreAppPINLogin` |  | public: login.default | anyone |
| GET | `/api/v1/store-app/pin` | `handleStoreAppPIN` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/store-app/pin` | `handleStoreAppPIN` |  | signed-in: store_app.default | every signed-in role |
| POST | `/api/v1/stored-value/cards/action` | `handleGiftCardAction` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/stored-value/cards` | `handleRegisterGiftCards` |  | signed-in: pos.default | every signed-in role |

### `public/view-pos.js`

Screens: POS (`#/view/pos`; help: Your first order, end to end, verified 2026-11-14; Open a shop and make your first sale, verified 2026-11-15; Point of Sale, verified 2026-11-16; A day as a cashier, verified 2026-08-17; A day as a store manager, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17).  
Owners: M05, M06, M19. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/availability` | `handleGetAvailability` |  | signed-in: availability.default | every signed-in role |
| POST | `/api/v1/checkout` | `handleCheckout` |  | signed-in: checkout.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/me` | `handleGetProfile` |  | signed-in: me.default | every signed-in role |
| PUT | `/api/v1/me` | `handleUpdateProfile` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/pos/quote` | `handlePOSQuote` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/pos/session/current` | `handlePOSSessionCurrent` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/session/open` | `handlePOSSessionOpen` |  | signed-in: pos.default | every signed-in role |

### `public/view-printing.js`

Screens: Sticker Printing (`#/view/stickers`; help: Printing - stickers and document templates, verified 2026-10-19; A day as a category manager, verified 2026-08-17; A day as a warehouse operator, verified 2026-08-17).  
Owners: M27. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/print/qz/printers` | `handleQZPrinters` | stickers | signed-in: stickers.default | every signed-in role |
| GET | `/api/v1/stickers/documents` | `handleStickerSourceDocs` | stickers | signed-in: stickers.default | every signed-in role |
| GET | `/api/v1/stickers/history` | `handlePrintHistory` | stickers | signed-in: stickers.default | every signed-in role |
| GET | `/api/v1/stickers/preview` | `handlePreviewStickers` | stickers | signed-in: stickers.default | every signed-in role |
| POST | `/api/v1/stickers/print` | `handlePrintStickers` | stickers | signed-in: stickers.default | every signed-in role |

### `public/view-privacy-requests.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/admin/privacy/requests/{id}/decide` | `handleDecideDataSubjectRequest` |  | admin: admin.privacy | Super Admin, Admin |
| POST | `/api/v1/admin/privacy/requests/{id}/execute` | `handleExecuteDataSubjectRequest` |  | admin: admin.privacy | Super Admin, Admin |
| GET | `/api/v1/admin/privacy/requests` | `handleListDataSubjectRequests` |  | admin: admin.privacy | Super Admin, Admin |
| POST | `/api/v1/admin/privacy/requests` | `handleCreateDataSubjectRequest` |  | admin: admin.privacy | Super Admin, Admin |

### `public/view-procurement.js`

Screens: Advance Shipment Notices (`#/view/asn`; help: Procurement, verified 2026-09-03; A day as a warehouse operator, verified 2026-08-17), Purchase Orders (`#/view/purchase-orders`; help: Open a shop and make your first sale, verified 2026-11-15; Procurement, verified 2026-09-03; A day as a store manager, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17), Goods Receipt (`#/view/grn`; help: Open a shop and make your first sale, verified 2026-11-15; Inventory & Warehouse Operations, verified 2026-11-15; Procurement, verified 2026-09-03; Batch, Serial & Expiry Traceability, verified 2026-11-15; A day as a store manager, verified 2026-08-17; A day as a warehouse operator, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17), Purchase Return (`#/view/purchase-returns`; help: Procurement, verified 2026-09-03).  
Owners: M03, M04. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/decide` | `handleDecideApproval` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/approval/pending` | `handleListPendingApprovals` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| POST | `/api/v1/crm/customer/merge` | `handleMergeCustomers` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/finance/charge-entries/bill` | `handleBillChargeEntries` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/grn-steps/open-pos` | `handleGRNStepsOpenPOs` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps/{id}/charges` | `handleGRNChargeLines` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps` | `handleGRNStepsInbox` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/inventory/item-setup` | `handleGetItemSetup` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/manufacturing/subcontract-order/receive` | `handleReceiveSubcontractOrder` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/manufacturing/subcontract-order/send` | `handleSendSubcontractOrder` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| GET | `/api/v1/procurement/contract/{id}/summary` | `handlePurchaseContractSummary` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/design-attribute` | `handleAddDesignAttributeValue` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/design-matrix` | `handleGetDesignMatrix` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/design/match` | `handleMatchDesign` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/design/{code}` | `handleGetDesign` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/preview` | `handlePreviewPurchaseOrder` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/line/{n}/close-rest` | `handlePOLineCloseRest` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/line/{n}/control` | `handlePOLineControl` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/line/{n}/stop` | `handlePOLineStop` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-order/{id}/reconciliation` | `handlePOReconciliation` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/short-close` | `handlePOShortClose` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/purchase-returns/context` | `handlePurchaseReturnContext` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-returns/{id}/post` | `handlePostPurchaseReturn` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |

### `public/view-product-sheet.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/pim/product-sheet/import` | `handleProductSheet` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/product-sheet/preview` | `handleProductSheet` | pim | signed-in: pim.default | every signed-in role |

### `public/view-profile.js`

Screens: Profile (`#/view/profile`; help: Your first sign-in, verified 2026-11-14; Security, Roles & Approvals, verified 2026-11-15).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/me/change-password` | `handleChangePassword` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/mfa/devices/forget` | `handleForgetMyMFADevices` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/mfa/recovery-codes/regenerate` | `handleMFARegenerateRecoveryCodes` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me/mfa/recovery-codes` | `handleMFARecoveryStatus` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/mfa/reenroll/cancel` | `handleMFAReenrollCancel` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/mfa/reenroll/confirm` | `handleMFAReenrollConfirm` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/mfa/reenroll` | `handleMFAReenrollStart` |  | signed-in: me.default | every signed-in role |
| GET | `/api/v1/me` | `handleGetProfile` |  | signed-in: me.default | every signed-in role |
| PUT | `/api/v1/me` | `handleUpdateProfile` |  | signed-in: me.default | every signed-in role |

### `public/view-purchase-contracts.js`

Screens: Contracts (`#/view/purchase-contracts`; help: Purchase contracts and service entries, verified 2026-11-14).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/procurement/contract/{id}/activate` | `handleActivatePurchaseContract` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/contract/{id}/close` | `handleClosePurchaseContract` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/contract/{id}/summary` | `handlePurchaseContractSummary` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-purchase-trust.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/procurement/purchase-rate-history` | `handlePurchaseRateHistory` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/vendor-scorecard` | `handleVendorScorecard` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-quality-scar.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/quality/aql-plan` | `handleAQLPlan` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/scar/{id}/step` | `handleAdvanceSCAR` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/scar` | `handleOpenSCAR` | quality | signed-in: quality.default | every signed-in role |

### `public/view-quality.js`

Owners: M15, M16. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/submit` | `handleSubmitApproval` |  | signed-in: approval.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/quality/coa/{id}/reject` | `handleRejectCertificateOfAnalysis` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/coa/{id}/release` | `handleReleaseCertificateOfAnalysis` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/coa` | `handleCreateCertificateOfAnalysis` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/inspection/{id}/dispose` | `handleDisposeQualityInspection` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/inspection` | `handleRecordQualityInspection` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/maintenance-order/{id}/cancel` | `handleCancelMaintenanceOrder` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/maintenance-order/{id}/complete` | `handleCompleteMaintenanceOrder` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/maintenance-order/{id}/schedule` | `handleScheduleMaintenanceOrder` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/maintenance-order/{id}/start` | `handleStartMaintenanceOrder` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/maintenance-order` | `handleCreateMaintenanceOrder` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/ncr/{id}/close` | `handleCloseNonConformanceReport` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/ncr/{id}/investigate` | `handleInvestigateNonConformanceReport` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/ncr/{id}/plan-corrective-action` | `handlePlanCorrectiveAction` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/quality/ncr` | `handleCreateNonConformanceReport` | quality | signed-in: quality.default | every signed-in role |

### `public/view-receipt.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/finance/receipts/open-items` | `handleCustomerOpenItems` |  | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/finance/receipts` | `handleCustomerReceipts` |  | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| POST | `/api/v1/finance/receipts` | `handleCustomerReceipts` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |

### `public/view-reports.js`

Screens: Reports (`#/view/reports`; help: Finding your way around, verified 2026-09-24; Keyboard shortcuts and quick codes, verified 2026-10-26; Open a shop and make your first sale, verified 2026-11-15; What this system is, verified 2026-08-17; CRM & Loyalty, verified 2026-11-15; Custom dashboards, verified 2026-10-22; Finance & Tax, verified 2026-11-15; Inventory & Warehouse Operations, verified 2026-11-15; Product Information Management, verified 2026-09-03; Point of Sale, verified 2026-11-16; Procurement, verified 2026-09-03; Batch, Serial & Expiry Traceability, verified 2026-11-15; Report catalog (part 2), verified 2026-09-10; Report catalog (part 3), verified 2026-09-10; Report catalog (part 4), verified 2026-09-10; Report catalog, verified 2026-09-10; A day as a category manager, verified 2026-08-17; A day in finance, verified 2026-08-17; A day as a store manager, verified 2026-08-17), Inventory (`#/view/inventory`; help: Finding your way around, verified 2026-09-24; Open a shop and make your first sale, verified 2026-11-15; Inventory & Warehouse Operations, verified 2026-11-15; A day as a store manager, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17).  
Owners: M25, M26. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/master-merges` | `handleMasterMerges` |  | admin: admin.master-merges | Super Admin, Admin |
| POST | `/api/v1/admin/master-merges` | `handleMasterMerges` |  | admin: admin.master-merges | Super Admin, Admin |
| ANY | `/api/v1/crm/segments` | `handleCRMSegments` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| DELETE | `/api/v1/dashboards/layouts/{id}` | `handleDeleteDashboardLayout` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/dashboards/layouts` | `handleDashboardLayouts` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/dashboards/layouts` | `handleDashboardLayouts` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/finance/reconciliation/exceptions` | `handleReconciliationException` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/inventory/stock` | `handleInventoryStock` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/pim/media/{id}/thumbnail` | `handlePIMMediaThumbnail` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/reports/catalog` | `handleReportCatalog` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/drilldown/{id}` | `handleReportDrillDown` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/reports/export/{id}/cancel` | `handleCancelReportExport` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/export/{id}` | `handleGetReportExport` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/reports/export` | `handleCreateReportExport` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/exports` | `handleListMyReportExports` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/gst-return-summary` | `handleGSTReturnSummary` | reports | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/reports/payables-ageing` | `handlePayablesAgeingReport` | reports | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/reports/receivables-ageing` | `handleReceivablesAgeingReport` | reports | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/reports/run/{id}` | `handleRunReport` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/runs/{id}` | `handleRecentReportRuns` | reports | signed-in: reports.default | every signed-in role |
| POST | `/api/v1/reports/schedules/{kind}/{id}/{action}` | `handleReportScheduleAction` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/schedules` | `handleListReportSchedules` | reports | signed-in: reports.default | every signed-in role |

### `public/view-reprint.js`

Screens: Reprint (`#/view/reprint`; help: Printing - stickers and document templates, verified 2026-10-19).  
Owners: M27. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/print/history` | `handleDocumentPrintHistory` |  | signed-in: doc.default | every signed-in role |

### `public/view-returns.js`

Screens: Returns (`#/view/returns`; help: Returns and refunds, verified 2026-11-15).  
Owners: M06. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/refunds/{id}/approve` | `handleApproveRefundRequest` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/refunds/{id}/process` | `handleProcessRefundRequest` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/returns/{id}/approve` | `handleApproveReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/returns/{id}/qc` | `handleApplyReturnQC` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/returns/{id}/receive` | `handleReceiveReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/returns/{id}/reject` | `handleRejectReturnRequest` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/returns/{id}/reverse-pickup` | `handleScheduleReturnReversePickup` | oms | signed-in: oms.default | every signed-in role |

### `public/view-rf-traceability.js`

Screens: RF Traceability (`#/view/rf-traceability`; help: RF lot and serial tasks, verified 2026-09-08; Warehouse (WMS) - what each screen is for, verified 2026-10-18).  
Owners: M08–M11. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/grn-steps/open-pos` | `handleGRNStepsOpenPOs` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps/{id}` | `handleGRNStepsGet` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/grn-steps` | `handleGRNStepsInbox` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/wms/batch/consume` | `handleBatchConsume` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/batch/expiry-sweep` | `handleBatchExpirySweep` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/batch/putaway` | `handleBatchPutaway` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/piece-codes` | `handlePieceCodeMint` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/serial/status` | `handleSerialStatus` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/work-queue` | `handleWorkQueue` | wms | signed-in: wms.default | every signed-in role |

### `public/view-rfq.js`

Screens: Request for Quotation (`#/view/rfq`; help: Procurement, verified 2026-09-03).  
Owners: M03, M04. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/rfq/save` | `handleSaveRFQ` | rfq | signed-in: rfq.default | every signed-in role |
| POST | `/api/v1/rfq/{id}/award` | `handleAwardRFQ` | rfq | signed-in: rfq.default | every signed-in role |
| GET | `/api/v1/rfq/{id}/comparison` | `handleRFQComparison` | rfq | signed-in: rfq.default | every signed-in role |
| POST | `/api/v1/rfq/{id}/decline` | `handleDeclineRFQ` | rfq | signed-in: rfq.default | every signed-in role |
| POST | `/api/v1/rfq/{id}/invite` | `handleInviteRFQVendors` | rfq | signed-in: rfq.default | every signed-in role |
| POST | `/api/v1/rfq/{id}/quote` | `handleRecordVendorQuote` | rfq | signed-in: rfq.default | every signed-in role |

### `public/view-roles.js`

Screens: Roles (`#/view/roles`; help: Security, Roles & Approvals, verified 2026-11-15; A day as an administrator, verified 2026-08-17; Which journey is mine, verified 2026-08-17).  
Owners: M29. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/role-permissions/changes` | `handleRoleGrantChanges` |  | admin: admin.role-permissions | Super Admin, Admin |
| POST | `/api/v1/admin/role-permissions/rollback` | `handleRoleGrantRollback` |  | admin: admin.role-permissions | Super Admin, Admin |
| GET | `/api/v1/admin/role-permissions` | `handleRolePermissions` |  | admin: admin.role-permissions | Super Admin, Admin |
| POST | `/api/v1/admin/role-permissions` | `handleRolePermissions` |  | admin: admin.role-permissions | Super Admin, Admin |
| GET | `/api/v1/admin/roles/landing` | `handleRoleLanding` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles/landing` | `handleRoleLanding` |  | admin: admin.roles | Super Admin, Admin |
| GET | `/api/v1/admin/roles/stores` | `handleRoleStores` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles/stores` | `handleRoleStores` |  | admin: admin.roles | Super Admin, Admin |
| GET | `/api/v1/admin/roles` | `handleListRoles` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles` | `handleCreateRole` |  | admin: admin.roles | Super Admin, Admin |
| GET | `/api/v1/admin/users` | `handleListUsers` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users` | `handleCreateUser` |  | admin: admin.users | Super Admin, Admin |

### `public/view-sales-history.js`

Screens: Sales History (`#/view/sales-history`; help: Point of Sale, verified 2026-11-16).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/pos/sales-history` | `handlePOSSalesHistory` |  | signed-in: pos.default | every signed-in role |
| GET | `/api/v1/returns/eligibility` | `handleReturnEligibility` | oms | signed-in: oms.default | every signed-in role |

### `public/view-sales-invoice.js`

Owners: M17, M18. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/pricing/line` | `handlePricingLine` |  | signed-in: pos.default | every signed-in role |

### `public/view-security-notify.js`

Screens: Privacy approvals (`#/view/privacy-approvals`; help: Privacy approvals, verified 2026-11-14).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/admin/security/export/backfill` | `handleSecurityExportBackfill` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/export/test` | `handleSecurityExportTest` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/export` | `handleSecurityExportStatus` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/export` | `handleSecurityExportSave` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/notify/approvers` | `handleNotifyApprovers` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/notify/decide` | `handleNotifyDecide` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/notify/decisions` | `handleNotifyDecisions` |  | admin: admin.privacy-approval | Super Admin, Admin, Privacy Approver |
| POST | `/api/v1/admin/security/notify/incidents` | `handleNotifyIncidentOpen` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/notify/matrix` | `handleNotifyMatrix` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/notify/matrix` | `handleNotifyMatrixSave` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/notify/review` | `handleNotifyReview` |  | admin: admin.privacy-approval | Super Admin, Admin, Privacy Approver |

### `public/view-security.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/audit-logs/forensic-export` | `handleForensicExport` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/audit-logs/seal-legacy` | `handleSealLegacyAudit` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/access-reviews` | `handleListAccessReviews` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/security/access-reviews` | `handleRecordAccessReview` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/security/detections/run` | `handleRunSecurityDetections` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/freeze-exports` | `handleFreezeExports` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/freeze-integrations` | `handleFreezeIntegrations` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/posture` | `handleSecurityPosture` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/read-only` | `handleReadOnlyMode` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/sessions/end` | `handleEndPrivilegedSession` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/sessions` | `handleListPrivilegedSessions` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/sign-out-all` | `handleSignOutAllUsers` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/security/sign-out-user` | `handleSignOutUserEverywhere` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/security/trail` | `handleSecurityTrail` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/users` | `handleListUsers` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users` | `handleCreateUser` |  | admin: admin.users | Super Admin, Admin |

### `public/view-service-desk.js`

Owners: M21–M24. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/projects/milestone/complete` | `handleCompleteProjectMilestone` | finance | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/projects/revenue/run` | `handleRunProjectRevenue` | finance | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/projects/{id}/ledger` | `handleProjectLedger` | finance | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/service/contracts/{id}` | `handleServiceContractView` | service | signed-in: service.default | every signed-in role |
| GET | `/api/v1/service/desk` | `handleServiceDesk` | service | signed-in: service.default | every signed-in role |
| POST | `/api/v1/service/ticket` | `handleCreateServiceTicket` | service | signed-in: service.default | every signed-in role |

### `public/view-service-entry.js`

Screens: Service entries (`#/view/service-entry`; help: Purchase contracts and service entries, verified 2026-11-14).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/procurement/service-entry/po/{id}` | `handleServiceBalance` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/service-entry/{id}/accept` | `handleAcceptServiceEntrySheet` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-set-off.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/finance/set-off/pairs/{id}/balances` | `handleSetOffBalances` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/finance/set-off/suggestions` | `handleSetOffSuggestions` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/set-off/{id}/reverse` | `handleSetOffReverse` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/set-off/{id}/statement` | `handleSetOffStatement` |  | signed-in: finance.default | every signed-in role |

### `public/view-setup-workbook.js`

Owners: M01, M30. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/admin/setup-workbook/apply` | `handleSetupWorkbookApply` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/setup-workbook/export` | `handleSetupWorkbookExport` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |
| POST | `/api/v1/admin/setup-workbook/preview` | `handleSetupWorkbookPreview` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/setup-workbook/runs/{id}/file` | `handleSetupWorkbookRunFile` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/setup-workbook/runs/{id}` | `handleSetupWorkbookRun` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/setup-workbook/runs` | `handleSetupWorkbookRuns` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |
| GET | `/api/v1/admin/setup-workbook/template` | `handleSetupWorkbookTemplate` |  | admin: admin.setup-workbook | Super Admin, Admin, Setup Operator |

### `public/view-shopify.js`

Owners: M12, M13, M28. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/integrations/credentials/disconnect` | `handleConnectorDisconnect` |  | admin: admin.integrations | Super Admin, Admin |
| GET | `/api/v1/shopify/{channel}` | `handleShopifyOverview` | oms | signed-in: oms.default | every signed-in role |

### `public/view-stock-age.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/stock-age-inspections` | `handleStockAgeInspections` | quality | signed-in: quality.default | every signed-in role |
| POST | `/api/v1/stock-age-inspections` | `handleStockAgeInspections` | quality | signed-in: quality.default | every signed-in role |

### `public/view-stock-locations.js`

Screens: Stock Locations (`#/view/stock-locations`; help: Inventory & Warehouse Operations, verified 2026-11-15), Move Stock (`#/view/stock-moves`; help: Inventory & Warehouse Operations, verified 2026-11-15).  
Owners: M07, M10, M20. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/stock-locations/balance` | `handleStockLocationBalance` |  | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/stock-locations/defaults` | `handleStockLocationDefault` |  | admin: admin.stock_locations | Super Admin, Admin |
| POST | `/api/v1/stock-locations/move` | `handleMoveStock` |  | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/stock-locations/stores` | `handleStoreStockLocations` |  | admin: admin.stock_locations | Super Admin, Admin |
| GET | `/api/v1/stock-locations` | `handleStockLocations` |  | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/stock-locations` | `handleStockLocations` |  | admin: admin.stock_locations | Super Admin, Admin |

### `public/view-store.js`

Owners: M07, M10, M20. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/roles/stores` | `handleRoleStores` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles/stores` | `handleRoleStores` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/users/location` | `handleSetUserLocation` |  | admin: admin.users | Super Admin, Admin |
| GET | `/api/v1/admin/users/stores` | `handleUserStores` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users/stores` | `handleUserStores` |  | admin: admin.users | Super Admin, Admin |
| GET | `/api/v1/me/stores` | `handleMyStores` |  | signed-in: me.default | every signed-in role |
| POST | `/api/v1/me/working-store` | `handleSwitchWorkingStore` |  | signed-in: me.default | every signed-in role |

### `public/view-subscription.js`

Screens: Subscription (`#/view/subscription`; help: Your Subscription, verified 2026-11-15; Settings and configuration, verified 2026-11-16).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/saas/subscription` | `handleMySubscription` |  | admin: finance.default | Super Admin, Admin |

### `public/view-supplier-portal.js`

Screens: Supplier portal (`#/view/supplier-portal`; help: The supplier portal, verified 2026-11-14).  
Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/supplier-portal/asn` | `handleSupplierPortalSendASN` | procurement | signed-in: supplier_portal.default | every signed-in role |
| GET | `/api/v1/supplier-portal/asns` | `handleSupplierPortalASNs` | procurement | signed-in: supplier_portal.default | every signed-in role |
| GET | `/api/v1/supplier-portal/bills` | `handleSupplierPortalBills` | procurement | signed-in: supplier_portal.default | every signed-in role |
| GET | `/api/v1/supplier-portal/orders` | `handleSupplierPortalOrders` | procurement | signed-in: supplier_portal.default | every signed-in role |
| GET | `/api/v1/supplier-portal/payments` | `handleSupplierPortalPayments` | procurement | signed-in: supplier_portal.default | every signed-in role |

### `public/view-transfer-allocation.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/transfer/allocation` | `handleTransferAllocationRun` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/allocation` | `handleTransferAllocationUpload` | inventory | signed-in: inventory.default | every signed-in role |

### `public/view-transfers.js`

Screens: Stock Movement (`#/view/transfers`; help: Inventory & Warehouse Operations, verified 2026-11-15; A day as a store manager, verified 2026-08-17; A day as a warehouse operator, verified 2026-08-17; Troubleshooting by symptom, verified 2026-08-17).  
Owners: M07, M10, M20. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/decide` | `handleDecideApproval` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/transfer/approve-direct` | `handleApproveTransferOrderDirect` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/cancel` | `handleCancelTransferOrder` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/transfer/detail` | `handleTransferDetail` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/dispatch` | `handleDispatchTransferOrder` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/receive` | `handleReceiveTransferOrder` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/return` | `handleReturnTransferToSource` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/submit` | `handleSubmitTransferOrder` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/wms/cartonization/suggest` | `handleCartonizationSuggest` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/transfer/pack` | `handlePackTransferOrder` | wms | signed-in: wms.default | every signed-in role |

### `public/view-users.js`

Screens: Users (`#/view/users`; help: Security, Roles & Approvals, verified 2026-11-15; A day as an administrator, verified 2026-08-17).  
Owners: M29. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/roles` | `handleListRoles` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/roles` | `handleCreateRole` |  | admin: admin.roles | Super Admin, Admin |
| POST | `/api/v1/admin/users/location` | `handleSetUserLocation` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users/reset-mfa` | `handleAdminResetUserMFA` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users/reset-password` | `handleRequestPasswordReset` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users/status` | `handleSetUserStatus` |  | admin: admin.users | Super Admin, Admin |
| GET | `/api/v1/admin/users/stores` | `handleUserStores` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users/stores` | `handleUserStores` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users/supplier` | `handleSetUserSupplier` |  | admin: admin.users | Super Admin, Admin |
| GET | `/api/v1/admin/users` | `handleListUsers` |  | admin: admin.users | Super Admin, Admin |
| POST | `/api/v1/admin/users` | `handleCreateUser` |  | admin: admin.users | Super Admin, Admin |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |

### `public/view-vendor-advance.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/procurement/combine-requisitions` | `handleCombineRequisitions` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/vendor-advances/{id}/apply` | `handleApplyVendorAdvance` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-advances/{id}/cancel` | `handleCancelVendorAdvance` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-advances/{id}/refund` | `handleRefundVendorAdvance` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-advances` | `handleGiveVendorAdvance` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-rebates/calculate` | `handleCalculateVendorRebates` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-rebates/{id}/settle` | `handleSettleVendorRebateClaim` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/vendor-rebates/{id}/submit` | `handleSubmitVendorRebateClaim` | procurement | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |

### `public/view-vendor-link.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/procurement/purchase-order/{id}/vendor-link` | `handleVendorLinkState` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/vendor-link` | `handleVendorLinkCreate` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/vendor-link/file/{id}/review` | `handleVendorLinkFileReview` | procurement | signed-in: procurement.default | every signed-in role |
| GET | `/api/v1/procurement/vendor-link/file/{id}` | `handleVendorLinkFile` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/vendor-link/proposal/{id}/apply` | `handleVendorLinkApply` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/vendor-link/proposal/{id}/reject` | `handleVendorLinkReject` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-vendor-onboarding.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| POST | `/api/v1/approval/decide` | `handleDecideApproval` |  | signed-in: approval.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/procurement/vendor-onboarding/request` | `handleVendorOnboardingRequest` | procurement | signed-in: procurement.default | every signed-in role |

### `public/view-warehouse.js`

Screens: Putaway (`#/view/putaway`; help: Inventory & Warehouse Operations, verified 2026-11-15; Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17), Warehouse Cockpit (`#/view/warehouse-cockpit`; help: Inventory & Warehouse Operations, verified 2026-11-15; Warehouse (WMS) - what each screen is for, verified 2026-10-18), Appointment Calendar (`#/view/appointment-calendar`; help: Dock appointments, verified 2026-09-09; Warehouse (WMS) - what each screen is for, verified 2026-10-18), Yard Board (`#/view/yard-board`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18; Trailer check-in and yard movement, verified 2026-09-09), Place Hold (`#/view/place-hold`; help: Place and release an inventory hold, verified 2026-10-18; Warehouse (WMS) - what each screen is for, verified 2026-10-18), RF Receiving (`#/view/rf-receiving`; help: Inventory & Warehouse Operations, verified 2026-11-15; Warehouse (WMS) - what each screen is for, verified 2026-10-18), Sortation (`#/view/sortation`; help: Sort orders into put-wall slots, verified 2026-09-09; Warehouse (WMS) - what each screen is for, verified 2026-10-18), Loading Dock (`#/view/loading-dock`; help: Load cartons and record departure, verified 2026-09-09; Warehouse (WMS) - what each screen is for, verified 2026-10-18), Bin Conditions (`#/view/bin-conditions`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17), Cycle Count (`#/view/cycle-count`; help: Inventory & Warehouse Operations, verified 2026-11-15; Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17), License Plates (`#/view/lpn`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17), Bin Replenishment (`#/view/bin-replenishment`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17), Location Movement (`#/view/location-movement`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18), Wave Picking (`#/view/wave-picking`; help: Inventory & Warehouse Operations, verified 2026-11-15; Batch, Serial & Expiry Traceability, verified 2026-11-15; Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17), Mobile Picking (`#/view/mobile-picking`; help: Warehouse (WMS) - what each screen is for, verified 2026-10-18; A day as a warehouse operator, verified 2026-08-17).  
Owners: M08–M11. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/availability` | `handleGetAvailability` |  | signed-in: availability.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/fulfillment/task/transition` | `handleFulfillmentTaskTransition` | wms_integration | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/stock-locations` | `handleStockLocations` |  | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/stock-locations` | `handleStockLocations` |  | admin: admin.stock_locations | Super Admin, Admin |
| GET | `/api/v1/wms/bin-contents` | `handleBinContents` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/bin-move` | `handleBinMove` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/bin-replenishment/execute` | `handleBinReplenishmentExecute` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/bin-replenishment/suggestions` | `handleBinReplenishmentSuggestions` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/cockpit` | `handleWarehouseCockpit` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/condition-transition` | `handleBinConditionTransition` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cross-dock/check` | `handleCrossDockCheck` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cross-dock/planned-putaway` | `handlePlannedCrossDockPutaway` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cross-dock/putaway` | `handleCrossDockPutaway` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/cycle-count/abc-plan` | `handleABCCycleCountPlan` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cycle-count/post-adjustment` | `handleRetryCycleCountPost` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cycle-count/reconcile` | `handleReconcileCycleCount` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cycle-count/recount/request` | `handleRequestRecount` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cycle-count/recount/submit` | `handleSubmitRecountValue` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/cycle-count/session` | `handleCycleCountSessionLines` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cycle-count/variance-reason` | `handleSetCycleCountVarianceReason` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/hold/place` | `handlePlaceHold` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/inbound-work` | `handleInboundWork` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/loading/bol` | `handleBillOfLading` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/loading/complete` | `handleLoadingComplete` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/loading/create` | `handleLoadingTaskCreate` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/loading/depart` | `handleLoadingDepart` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/loading/scan` | `handleLoadingScan` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/lpn/assign` | `handleLPNAssign` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/lpn/contents` | `handleLPNContents` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/outbound-work` | `handleOutboundWork` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/pick-list` | `handlePickList` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/putaway/suggest-bin` | `handleSuggestPutawayBin` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/putaway` | `handlePutaway` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/sortation/assign-wave` | `handleSortSlotAssignWave` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/sortation/assign` | `handleSortSlotAssign` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/sortation/clear` | `handleSortSlotClear` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/sortation/confirm` | `handleSortSlotConfirm` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/sortation/provision-slots` | `handleSortSlotProvision` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/sortation/slots` | `handleSortSlotList` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/wave/from-selection` | `handleWaveFromSelection` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/wave/pick-confirm` | `handleWavePickConfirm` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/wave/pick-list` | `handleWavePickList` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/wave/template/run` | `handleWaveTemplateRun` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/wave/transition` | `handleWaveTransition` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/yard/check-in` | `handleYardCheckIn` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/yard/{id}/door` | `handleYardAssignDoor` | wms | signed-in: wms.default | every signed-in role |

### `public/view-wholesale.js`

Owners: not in the plan's table. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/finance/summary-invoices/preview` | `handlePreviewSummaryInvoices` | wholesale | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/summary-invoices` | `handleCreateSummaryInvoices` | wholesale | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/procurement/dropship-confirm` | `handleConfirmDropShip` | wholesale | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/procurement/dropship-lines` | `handleOpenDropShipLines` | wholesale | signed-in: wholesale.default | every signed-in role |

### `public/view-wms-labour.js`

Owners: M08–M11. Live inspection: not recorded.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/wms/billing/invoices` | `handleCapturedChargeInvoice` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/billing/review` | `handleBillingReview` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/billing/storage/capture` | `handleCaptureStorageCharges` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/labor/plan` | `handleLaborPlan` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/labor/standard-preview` | `handleLaborStandardPreview` | wms | signed-in: wms.default | every signed-in role |

No screen and no API call: `api-claim.js`, `api-docs-viewer.js`, `api-docs.js`, `api-key-once.js`, `charts.js`, `db.js`, `print-pdf.js`, `shell-nav.js`, `shell-steady.js`, `store/sw.js`, `templates-page.js`, `theme-boot.js`, `view-api-access-grid.js`, `view-crm-messaging.js`, `view-crm-sales.js`, `view-crm-service.js`, `view-document-links.js`.

## Screens missing an owner or a help article

13 of 79 screens. An owner comes from the plan's page ownership table, an article from a Knowledge Center article's `screens:` list.

| Screen | View | File | Owner | Help article |
|---|---|---|---|---|
| Bank Files | `bank-files` | `public/view-bank-files.js` | missing | yes |
| Bin Generator | `bin-tools` | `public/view-bin-tools.js` | missing | yes |
| Data Healer | `datahealer` | `public/view-data-healer.js` | missing | yes |
| Data quality | `dataquality` | `public/view-data-quality.js` | missing | yes |
| Gate entries | `gate-entry` | `public/view-gate-entry.js` | missing | yes |
| Offline Sales | `offline-sales` | `public/view-offline-sales.js` | missing | yes |
| Profile | `profile` | `public/view-profile.js` | missing | yes |
| Contracts | `purchase-contracts` | `public/view-purchase-contracts.js` | missing | yes |
| Sales History | `sales-history` | `public/view-sales-history.js` | missing | yes |
| Privacy approvals | `privacy-approvals` | `public/view-security-notify.js` | missing | yes |
| Service entries | `service-entry` | `public/view-service-entry.js` | missing | yes |
| Subscription | `subscription` | `public/view-subscription.js` | missing | yes |
| Supplier portal | `supplier-portal` | `public/view-supplier-portal.js` | missing | yes |

## Generic Setup pages

Each record type is one generic list and form (`#/view/doctype-table`, `view-documents.js` and the shell's document form), owned per the plan by the shared contract across M01-M30. Who may read or change a record type is the role's per-doctype grant (Settings → Roles), on top of the routes below. Record types from the data registry snapshot captured 2026-09-09; types added since appear at the next capture.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| ANY | `/api/v1/doc/{doctype}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/doc/{doctype}/meta` | `handleGetDocTypeMeta` |  | signed-in: doc.default | every signed-in role |
| ANY | `/api/v1/doc/{doctype}/{id}` | `handleGenericDoc` |  | signed-in: doc.default | every signed-in role |
| POST | `/api/v1/doc/{doctype}/{id}/reactivate` | `handleReactivateMasterDocument` |  | signed-in: doc.default | every signed-in role |

| Module | Record types |
|---|---|
| CRM | Campaign, LoyaltyRedemptionRequest, Voucher |
| Core | CostCenter, Department, HelpArticleFeedback, LegalEntity, Location |
| Finance | Asset, BackdatedPostingRequest, BankAccount, BankStatementLine, Budget, CreditNote, Currency, DebitNote, DeferredRevenueSchedule, ExchangeRate, ExpenseClaim, GLPost, IntercompanyTransaction, JournalVoucher, LandedCostVoucher, MarketplaceSettlement, MarketplaceSettlementLine, PaymentProposal, PrepaidExpenseSchedule, Project, TDSSection |
| HR | Appraisal, AppraisalCycle, Attendance, Employee, EmployeeLoan, Grievance, Leave, OnboardingChecklist, Payslip, SalaryStructure, Shift, ShiftAssignment, TrainingProgram, TrainingRecord |
| Inbound | ASN |
| Integrations | WebhookSubscription |
| Inventory | AllocationStrategy, Appointment, Bin, BinReplenishmentRule, CapturedCharge, CartonType, ChargeCode, ChargeContract, ChargeGroup, CrossDockPlan, CycleClass, CycleCountLine, DockDoor, FloorAssistRequest, FulfillmentTask, Hold, HoldCode, HoldReleaseRequest, Item, LPN, LaborAllowance, LaborElement, LaborOperation, LaborStandard, LoadingTask, LogisticsBooking, PackStation, PackTemplate, PackingValidationTemplate, PhysicalInventory, PreShipValidationRule, Printer, PutawayStrategy, RateGroup, ReceiptValidationRule, ReorderPointConfig, RoboticsIntegrationCredential, SortSlot, SortStation, StockLedgerEntry, StorageBalanceSnapshot, StorageBillingRate, TaskCompletionLog, TaskDispatchStrategy, Trailer, TransferOrder, TravelSection, UserWorkSchedule, WarehouseTask, Wave, WaveTemplate, WeeklySchedule, YardCheckIn, Zone |
| Manufacturing | BOM, ProductionOrder, QualityInspection, Routing, SubcontractOrder, WorkCenter |
| Master Data | Batch, Brand, Color, LottableConstraint, Model, SerialNumber, Size, Style, UOM, UOMConversion |
| OMS | AllocationRule, BundleAssembly, ChannelSKUException, ChannelSyncRun, CourierServiceArea, CourierTrackingEvent, GatePass, Manifest, NDRCase, NotificationChannelConfig, NotificationLog, NotificationTemplate, OMSSavedView, ProductBundle, ReasonCode, RefundRequest, ReturnRequest, SalesOrder, SalesOrderLine, ShippingPackage, StatusTransitionRule |
| PIM | Channel, ChannelCategoryMap, ChannelFieldMap, ChannelValidationRule, CompetitorPrice, ContentAssistLog, ImportJob, PIMCatalog, PIMExportSchedule, PIMExportTemplate, PIMImportSchedule, PIMImportTemplate, PIMProductGroup, PIMProductProfile, PIMTask, PIMTaskTemplate, PIMTransformRule, PIMWorkflowDefinition, PIMWorkflowRun, ProductAttributeDef, ProductAttributeGroup, ProductAttributeValue, ProductContent, ProductFamily, ProductFamilyAttribute, ProductMedia, SupplierSubmission |
| POS | Offer, POSInvoice |
| Procurement | GRN, PurchaseOrder, PurchaseRequisition, PurchaseRequisitionDescription, RFQ, Vendor, VendorInvoice, VendorQuote |
| Quality | CertificateOfAnalysis, InspectionPlan, MaintenanceOrder, MaintenanceSchedule, NonConformanceReport |
| Reports | DashboardDigest, DashboardLayout, ReportColumnProfile, ReportExportJob, ReportFilterPreset, ReportRunLog, ScheduledReport |
| Sales | Customer, POSCart, POSCostingGap, POSOfflineQueueGap, POSOfflineSyncVariance, POSPriceOverride, POSProfile, POSSession, PriceListVersion, RecurringSalesContract, SalesInvoice, SalesReturn |
| Service | ServiceContract, ServiceTicket |

## Configuration settings

Settings → Configuration renders the settings registry by module, through the routes below.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/settings` | `handleGetSettings` |  | admin: admin.settings | Super Admin, Admin, Setup Operator |
| PUT | `/api/v1/admin/settings` | `handleUpdateSettings` |  | admin: admin.settings | Super Admin, Admin, Setup Operator |
| POST | `/api/v1/shopify/{channel}/settings` | `handleShopifySettings` | oms | signed-in: oms.default | every signed-in role |

| Module | Settings |
|---|---|
| App branding | 1 |
| Assets | 1 |
| CRM | 30 |
| Expenses | 3 |
| Finance | 13 |
| GST & Compliance | 1 |
| HR | 4 |
| HR & Payroll | 3 |
| Imports | 1 |
| Inventory | 21 |
| Localization | 2 |
| Loyalty | 14 |
| Manufacturing | 6 |
| Navigation | 2 |
| Notifications | 1 |
| OMS | 10 |
| Orders | 11 |
| PIM | 8 |
| PIM / PIMS | 2 |
| Platform | 30 |
| Platform billing | 1 |
| Point of Sale | 14 |
| Printing | 6 |
| Procurement | 22 |
| Procurement & GRN | 15 |
| Quality | 4 |
| Reports | 1 |
| Sales | 3 |
| Sales & Returns | 5 |
| Security | 30 |
| Service | 4 |
| Stock Transfer | 1 |
| Store app | 1 |
| Warehouse | 16 |

## Routes no front-end file calls

Integration, device, webhook or customer-link routes, calls built in a way the scan cannot read, or routes nothing uses. Inspect before relying on either.

| Method | Route | Handler | Gate | Capability | Roles |
|---|---|---|---|---|---|
| GET | `/api/v1/admin/access-preview` | `handleAccessPreview` |  | admin: admin.access-preview | Super Admin, Admin |
| POST | `/api/v1/admin/api-credentials` | `handleIssueAPICredential` |  | admin: admin.api-credentials | Super Admin, Admin |
| GET | `/api/v1/admin/api-credentials` | `handleListAPICredentials` |  | admin: admin.api-credentials | Super Admin, Admin |
| PUT | `/api/v1/admin/api-credentials/{id}/limits` | `handleSetAPICredentialLimits` |  | admin: admin.api-credentials | Super Admin, Admin |
| POST | `/api/v1/admin/api-credentials/{id}/rotate` | `handleRotateAPICredential` |  | admin: admin.api-credentials | Super Admin, Admin |
| GET | `/api/v1/admin/api-credentials/{id}/traffic` | `handleListAPICredentialTraffic` |  | admin: admin.api-credentials | Super Admin, Admin |
| GET | `/api/v1/admin/api-traffic` | `handleListAPICredentialTraffic` |  | admin: admin.api-traffic | Super Admin, Admin |
| POST | `/api/v1/admin/audit-logs/archive` | `handleRunAuditArchive` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/audit-logs/archives` | `handleListAuditArchives` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/audit-logs/archives/{id}` | `handleReadAuditArchive` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/audit-logs/archives/{id}/export` | `handleExportAuditArchive` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/audit-logs/archives/{id}/restore` | `handleRestoreAuditArchive` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/audit-logs/checkpoint` | `handleWriteAuditCheckpoint` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/audit-logs/evidence` | `handleVerifyAuditEvidence` |  | admin: admin.audit-logs | Super Admin, Admin |
| GET | `/api/v1/admin/audit-logs/verify` | `handleVerifyAuditLogChain` |  | admin: admin.audit-logs | Super Admin, Admin |
| POST | `/api/v1/admin/master-merges/{id}/undo` | `handleMasterMergeUndo` |  | admin: admin.master-merges | Super Admin, Admin |
| GET | `/api/v1/admin/modules` | `handleListModules` |  | admin: admin.modules | Super Admin |
| POST | `/api/v1/admin/patch/approve` | `handleApprovePatchProposal` |  | admin: admin.patch | Super Admin |
| GET | `/api/v1/admin/patch/proposals` | `handleListPatchProposals` |  | admin: admin.patch | Super Admin |
| POST | `/api/v1/admin/patch/reject` | `handleRejectPatchProposal` |  | admin: admin.patch | Super Admin |
| POST | `/api/v1/admin/privacy/legal-hold` | `handleSetSubjectLegalHold` |  | admin: admin.privacy | Super Admin, Admin |
| GET | `/api/v1/admin/public-api/openapi.json` | `handlePublicAPIOpenAPISpec` |  | admin: admin.public-api | Super Admin, Admin |
| GET | `/api/v1/admin/role-templates` | `handleListRoleTemplates` |  | admin: admin.role-templates | Super Admin, Admin |
| POST | `/api/v1/admin/role-templates/apply` | `handleApplyRoleTemplates` |  | admin: admin.role-templates | Super Admin, Admin |
| GET | `/api/v1/admin/role-templates/migrations` | `handleListRoleTemplateMigrations` |  | admin: admin.role-templates | Super Admin, Admin |
| GET | `/api/v1/admin/role-templates/plan` | `handleRoleTemplatePlan` |  | admin: admin.role-templates | Super Admin, Admin |
| POST | `/api/v1/admin/role-templates/revert` | `handleRevertRoleTemplates` |  | admin: admin.role-templates | Super Admin, Admin |
| GET | `/api/v1/admin/saas/costs` | `handleSaaSCosts` |  | admin: admin.tenant | Super Admin |
| GET | `/api/v1/admin/saas/plan` | `handleSaaSPlans` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/saas/plan` | `handleSaaSPlans` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/saas/reconcile` | `handleSaaSReconcile` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/saas/subscribe` | `handleSaaSSubscribe` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/admin/sandbox-tenants` | `handleProvisionSandboxTenant` |  | admin: admin.sandbox-tenants | Super Admin |
| GET | `/api/v1/admin/sandbox-tenants` | `handleListSandboxTenants` |  | admin: admin.sandbox-tenants | Super Admin |
| DELETE | `/api/v1/admin/sandbox-tenants/{id}` | `handleDeleteSandboxTenant` |  | admin: admin.sandbox-tenants | Super Admin |
| POST | `/api/v1/admin/sandbox-tenants/{id}/reset` | `handleResetSandboxTenant` |  | admin: admin.sandbox-tenants | Super Admin |
| POST | `/api/v1/admin/scale-test` | `handleScaleTest` |  | admin: admin.scale-test | Super Admin |
| GET | `/api/v1/admin/sod-conflicts` | `handleSoDCatalog` |  | admin: admin.sod-conflicts | Super Admin, Admin |
| POST | `/api/v1/admin/tenant/host-slug` | `handleSetTenantHostSlug` |  | admin: admin.tenant | Super Admin |
| GET | `/api/v1/admin/tenant/version` | `handleGetTenantVersion` |  | admin: admin.tenant | Super Admin |
| POST | `/api/v1/attachments/file/{attachmentID}/remove` | `handleRemoveAttachment` |  | signed-in: attachments.default | every signed-in role |
| GET | `/api/v1/clevertap/logs` | `handleListCleverTapLogs` |  | signed-in: clevertap.default | every signed-in role |
| ANY | `/api/v1/crm/appointments/{id}/{action}` | `handleCRMAppointments` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/campaign-runs/{id}/{action}` | `handleCRMCampaignRun` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/campaigns/{id}` | `handleCRMCampaigns` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/campaigns/{id}/{part}` | `handleCRMCampaigns` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/conversations/{id}` | `handleCRMConversations` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/conversations/{id}/{part}` | `handleCRMConversations` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/holds/{id}/{action}` | `handleCRMHolds` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/hooks/email` | `handleCRMHook` | crm_loyalty | public: crm_loyalty.default | anyone |
| ANY | `/api/v1/crm/hooks/sms` | `handleCRMHook` | crm_loyalty | public: crm_loyalty.default | anyone |
| ANY | `/api/v1/crm/hooks/webhook` | `handleCRMHook` | crm_loyalty | public: crm_loyalty.default | anyone |
| ANY | `/api/v1/crm/hooks/whatsapp` | `handleCRMHookWhatsApp` | crm_loyalty | public: crm_loyalty.default | anyone |
| ANY | `/api/v1/crm/journeys/{id}` | `handleCRMJourneys` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/journeys/{id}/{part}` | `handleCRMJourneys` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/leads/{id}` | `handleCRMLeads` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/leads/{id}/{part}` | `handleCRMLeads` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/loyalty-redemption/initiate` | `handleInitiateSecureLoyaltyRedemption` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/loyalty-redemption/verify` | `handleVerifySecureLoyaltyRedemption` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/messages/{id}/{part}` | `handleCRMMessages` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/opportunities/{id}` | `handleCRMOpportunities` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/opportunities/{id}/{part}` | `handleCRMOpportunities` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/pipeline/{part}` | `handleCRMPipelineConfig` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/public/unsubscribe` | `handleCRMPublicUnsubscribe` | crm_loyalty | public: crm_loyalty.default | anyone |
| ANY | `/api/v1/crm/referrals/{id}/{part}` | `handleCRMReferrals` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/segments/{id}` | `handleCRMSegments` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/senders/{id}` | `handleCRMSenders` | crm_loyalty | admin: crm_loyalty.default | Super Admin, Admin |
| POST | `/api/v1/crm/senders/{id}/test` | `handleCRMSenderTest` | crm_loyalty | admin: crm_loyalty.default | Super Admin, Admin |
| ANY | `/api/v1/crm/stores/{store}/slots` | `handleCRMStoreSlots` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/crm/stores/{store}/slots/{day}` | `handleCRMStoreSlots` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/surveys/{id}` | `handleCRMSurveys` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/surveys/{id}/{part}` | `handleCRMSurveys` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tasks/{id}` | `handleCRMTasks` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/templates/{id}` | `handleCRMTemplates` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/templates/{id}/{part}` | `handleCRMTemplates` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tickets/{id}` | `handleCRMTickets` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| ANY | `/api/v1/crm/tickets/{id}/{part}` | `handleCRMTickets` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/voucher-holds/{action}` | `handleCRMVoucherHold` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/voucher/redeem` | `handleRedeemVoucher` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/crm/voucher/validate` | `handleValidateVoucher` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/data-quality/{id}/status` | `handleSetDataQualityStatus` |  | signed-in: master.data-quality | every signed-in role |
| ANY | `/api/v1/debug/panic` | `handleDebugPanic` |  | signed-in: debug.default | every signed-in role |
| POST | `/api/v1/doc/{doctype}/{id}/reactivate` | `handleReactivateMasterDocument` |  | signed-in: doc.default | every signed-in role |
| GET | `/api/v1/einvoice/reconciliation` | `handleEInvoiceReconciliation` | finance | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| POST | `/api/v1/finance/intercompany-transaction/{id}/retry-post` | `handleRetryPostIntercompanyTransaction` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/journal-voucher/recurring` | `handleCreateRecurringJournalTemplate` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/landed-cost-voucher` | `handleCreateLandedCostVoucher` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| POST | `/api/v1/finance/landed-cost-voucher/{id}/apply` | `handleApplyLandedCostVoucher` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/payment-proposal/{id}/payment-file` | `handleGeneratePaymentFile` |  | signed-in: finance.default | every signed-in role |
| POST | `/api/v1/finance/payment-proposal/{id}/record-utr` | `handleRecordPaymentUTR` |  | signed-in: finance.post | Super Admin, Admin, Administrator, Accounts Payable, Accounts Receivable, Accountant |
| GET | `/api/v1/finance/payment-proposal/{id}/utrs` | `handleListPaymentUTRs` |  | signed-in: finance.default | every signed-in role |
| GET | `/api/v1/finance/trial-balance/presentation` | `handleTrialBalancePresentationCurrency` |  | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| POST | `/api/v1/fulfillment/return` | `handleFulfillmentReturn` | wms / wms_integration | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/grn-steps/{id}/post/{child}` | `handleGRNStepsPostChild` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/gst/calculate` | `handleCalculateGST` |  | signed-in: gst.default | every signed-in role |
| GET | `/api/v1/help/public/article` | `handleHelpPublicArticle` |  | public: help.default | anyone |
| GET | `/api/v1/help/public/index` | `handleHelpPublicIndex` |  | public: help.default | anyone |
| GET | `/api/v1/help/public/search-index` | `handleHelpPublicSearchIndex` |  | public: help.default | anyone |
| GET | `/api/v1/hr/salary-components` | `handleSalaryComponentsPreview` | hr | signed-in: hr.payroll | Super Admin, Admin, Administrator, HR Manager |
| GET | `/api/v1/import-batches/{id}` | `handleImportBatchGet` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches/{id}/approve` | `handleImportBatchApprove` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches/{id}/commit` | `handleImportBatchCommit` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches/{id}/discard` | `handleImportBatchDiscard` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches/{id}/preview` | `handleImportBatchPreview` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches/{id}/reconcile` | `handleImportBatchReconcile` |  | signed-in: import.default | every signed-in role |
| GET | `/api/v1/import-batches/{id}/rows` | `handleImportBatchRows` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/import-batches/{id}/validate` | `handleImportBatchValidate` |  | signed-in: import.default | every signed-in role |
| GET | `/api/v1/import/{doctype}/load-order` | `handleImportLoadOrder` |  | signed-in: import.default | every signed-in role |
| POST | `/api/v1/integration/bigcommerce/webhook/{channelCode}` | `handleBigCommerceWebhook` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/integration/courier/{provider}/tracking` | `handleCourierTrackingWebhook` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/integration/razorpay/subscription-webhook` | `handleSaaSSubscriptionWebhook` |  | public: finance.default | anyone |
| POST | `/api/v1/integration/razorpay/webhook` | `handleRazorpayWebhook` |  | public: finance.default | anyone |
| POST | `/api/v1/integration/shopify/order` | `handleShopifyOrderWebhook` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/integration/shopify/product/map` | `handleShopifyProductMap` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/integration/shopify/webhook` | `handleShopifyWebhook` | oms | public: oms.default | anyone |
| POST | `/api/v1/integrations/clevertap/segment-sync` | `handleCleverTapSegmentSync` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| POST | `/api/v1/inventory/pieces/{doctype}/{id}/correct` | `handleCorrectPieceAttributes` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/loyalty/redeem` | `handleRedeemLoyaltyPoints` | crm_loyalty | signed-in: crm_loyalty.default | every signed-in role |
| GET | `/api/v1/manufacturing/active-bom` | `handleActiveBOMForItem` | manufacturing | signed-in: manufacturing.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/listings/link` | `handleChannelListingsLink` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/channels/{channel}/listings/link-preview` | `handleChannelListingsLinkPreview` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/channels/{channel}/listings/stock-sync` | `handleChannelListingsStockSync` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/connectors` | `handleConnectorDescriptors` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/couriers/{provider}/awb` | `handleCourierAllocateAWB` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/couriers/{provider}/cancel` | `handleCourierCancel` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/couriers/{provider}/pickup` | `handleCourierPickup` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/logistics/label.pdf` | `handleShippingLabelPDF` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/marketplace/logistics/serviceability` | `handleCourierServiceability` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/marketplace/ndr/{id}/resolve` | `handleNDRResolve` | oms / oms_integration | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/gate-passes/{id}/discard` | `handleGatePassTransition` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/gate-passes/{id}/update` | `handleUpdateGatePass` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/shipping-packages/{id}/cancel` | `handleCancelShippingPackage` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/oms/shipping-packages/{id}/split` | `handleSplitShippingPackage` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/optimization/forecast` | `handleDemandForecast` | advanced_forecasting | signed-in: optimization.default | every signed-in role |
| GET | `/api/v1/optimization/replenishment-suggestions` | `handleReplenishmentSuggestions` | advanced_forecasting | signed-in: optimization.default | every signed-in role |
| GET | `/api/v1/optimization/sla-breaches` | `handleSLABreaches` | advanced_forecasting | signed-in: optimization.default | every signed-in role |
| POST | `/api/v1/orders/{id}/hold` | `handlePlaceOrderHold` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/payment` | `handleRecordOrderPayment` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/priority` | `handleSetOrderPriority` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/release-to-fulfillment` | `handleReleaseOrderToFulfillment` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/split` | `handleSplitOrder` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/orders/{id}/switch-facility` | `handleSwitchOrderFacility` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/pim/channels/{code}/credentials` | `handleSaveChannelCredential` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/channels/{code}/pull-state` | `handlePIMChannelPullState` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/import-schedules/{id}/rotate-hook-token` | `handlePIMImportScheduleRotateHookToken` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/import/hook` | `handlePIMImportHook` | pim | public: pim.default | anyone |
| POST | `/api/v1/pim/import/{doctype}/preview` | `handlePIMImportPreview` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/market-harvest/reparse` | `handlePIMMarketHarvestReparse` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/market-harvest/runs` | `handlePIMMarketHarvestRun` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/media/transform-presets` | `handlePIMMediaTransformPresets` | pim | signed-in: pim.default | every signed-in role |
| GET | `/api/v1/pim/publish/{jobID}` | `handlePIMPublishJobStatus` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pim/workflow-runs/bulk` | `handlePIMWorkflowRunBulk` | pim | signed-in: pim.default | every signed-in role |
| POST | `/api/v1/pinelabs/reconcile` | `handlePineLabsReconcile` |  | signed-in: pinelabs.default | every signed-in role |
| POST | `/api/v1/pinelabs/transaction` | `handlePineLabsTransaction` |  | signed-in: pinelabs.default | every signed-in role |
| GET | `/api/v1/pinelabs/transactions` | `handleListPineLabsTransactions` |  | signed-in: pinelabs.default | every signed-in role |
| GET | `/api/v1/pos/loyalty/redemption/status` | `handlePOSRedemptionStatus` | crm_loyalty | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/payment/confirm` | `handlePOSPaymentConfirm` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/pos/payment/void` | `handlePOSPaymentVoid` |  | signed-in: pos.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/images/main` | `handlePOImageMain` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/images/remove` | `handlePOImageRemove` | procurement | signed-in: procurement.default | every signed-in role |
| POST | `/api/v1/procurement/purchase-order/{id}/vendor-link/revoke` | `handleVendorLinkRevoke` | procurement | signed-in: procurement.default | every signed-in role |
| ANY | `/api/v1/procurement/vendor-link/public/invoice` | `handleVendorLinkPublicInvoice` | procurement | public: procurement.default | anyone |
| ANY | `/api/v1/procurement/vendor-link/public/proposal` | `handleVendorLinkPublicProposal` | procurement | public: procurement.default | anyone |
| POST | `/api/v1/refunds/{id}/reject` | `handleRejectRefundRequest` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/reports/current-stock` | `handleCurrentStockReport` | reports | signed-in: reports.default | every signed-in role |
| GET | `/api/v1/reports/sales-register` | `handleSalesRegisterReport` | reports | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/reports/vendor-ledger` | `handleVendorLedgerReport` | reports | signed-in: finance.statements | Super Admin, Admin, Administrator, Store Supervisor, Accounts Payable, Accounts Receivable, Accountant, Auditor |
| GET | `/api/v1/rfq/quotes` | `handleGetVendorQuotesForRFQ` | rfq | signed-in: rfq.default | every signed-in role |
| POST | `/api/v1/rfq/select-quote` | `handleSelectWinningQuote` | rfq | signed-in: rfq.default | every signed-in role |
| POST | `/api/v1/security/csp-report` | `handleCSPReport` |  | public: security.default | anyone |
| POST | `/api/v1/shopify/{channel}/credentials` | `handleShopifyCredentials` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/shopify/{channel}/do/{action}` | `handleShopifyAction` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/shopify/{channel}/links` | `handleShopifyLinks` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/shopify/{channel}/mismatch` | `handleShopifyMismatch` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/shopify/{channel}/settings` | `handleShopifySettings` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/transfer/allocation/decide` | `handleTransferAllocationDecide` | inventory | signed-in: inventory.default | every signed-in role |
| POST | `/api/v1/transfer/cancel-lines` | `handleCancelTransferLines` | inventory | signed-in: inventory.default | every signed-in role |
| GET | `/api/v1/unicommerce/inventory-syncs` | `handleListUnicommerceInventorySyncs` | oms | signed-in: oms.default | every signed-in role |
| POST | `/api/v1/unicommerce/order` | `handleUnicommerceOrder` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/unicommerce/orders` | `handleListUnicommerceOrders` | oms | signed-in: oms.default | every signed-in role |
| GET | `/api/v1/wms/batch/allocation-preview` | `handleBatchAllocationPreview` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/batch/status` | `handleBatchStatus` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/billing/charges` | `handleCapturedCharges` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/billing/charges` | `handleCapturedCharges` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/billing/storage` | `handleStorageBillingV2` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/billing/storage/snapshot` | `handleStorageBalanceSnapshot` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/bin-replenishment/demand-driven` | `handleDemandDrivenReplenishmentSuggestions` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/bin-replenishment/dynamic-pickface` | `handleDynamicPickFaceSuggestions` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/bin-replenishment/dynamic-pickface/apply` | `handleApplyDynamicPickFaceMinMax` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/bin-replenishment/wave` | `handleWaveReplenishmentSuggestions` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/cartonization/suggest-v2` | `handleCartonizationSuggestV2` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/cycle-count/plan` | `handleCycleCountPlan` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/facility/children` | `handleFacilityChildren` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/facility/copy` | `handleFacilityCopy` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/facility/descendants` | `handleFacilityDescendants` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/lpn/deconsolidate` | `handleDeconsolidateLPN` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/owner-stock/assign` | `handleOwnerStockAssign` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/owner-stock/consume` | `handleOwnerStockConsume` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/owner-stock/correct` | `handleCorrectOwnerStock` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/owner/assign-warehouse` | `handleWarehouseOwnerAssign` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/owner/mixed-locations` | `handleMixedOwnerLocations` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/pack-template/resolve` | `handlePackTemplateResolve` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/physical-inventory/cancel` | `handlePhysicalInventoryCancel` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/physical-inventory/close` | `handlePhysicalInventoryClose` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/physical-inventory/reconcile` | `handlePhysicalInventoryReconcile` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/physical-inventory/start` | `handlePhysicalInventoryStart` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/physical-inventory/submit-count` | `handlePhysicalInventorySubmitCount` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/robotics/event` | `handleRoboticsEvent` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/serial/putaway` | `handleSerialPutaway` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/tasks/next` | `handleNextTask` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/tasks/transition` | `handleTransitionTask` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/vas/complete` | `handleVASTaskComplete` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/vas/create` | `handleVASTaskCreate` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/wave/assign` | `handleWaveAssign` | wms | signed-in: wms.default | every signed-in role |
| POST | `/api/v1/wms/wave/create` | `handleWaveCreate` | wms | signed-in: wms.default | every signed-in role |
| GET | `/api/v1/wms/wave/monitor` | `handleWaveMonitor` | wms | signed-in: wms.default | every signed-in role |

## Calls matching no registered route

A path in front-end source that no `/api` registration in routes.go matches: a comment, a path built from pieces, or a call that will fail.

| File | Call |
|---|---|
| `api-claim.js` | `/api/public/v1/developer-links/{}/claim` |
| `api-docs.js` | `/api/public/v1/docs/{}` |
| `app.js` | `/api/v` |
| `view-crm.js` | `/api/{}` |
