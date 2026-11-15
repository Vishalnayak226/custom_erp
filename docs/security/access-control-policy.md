---
doc_id: SEC-ACCESS-001
title: Access and segregation-of-duties policy
type: normative
status: approved
owner: security-owner
approvers: [security-owner, finance-process-owner, hr-process-owner, tenant-owner]
audience: [tenant-admins, security, process-owners, engineering]
applies_to: tenant-approved capability, field and scope policy
authority: canonical
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# Access and segregation-of-duties policy

Evaluate route capability, object/action, tenant, entity/location/owner/self scope, field
classification and workflow state server-side before returning data or accepting work.
Apply the same policy to search/count/export, attachments, jobs, imports and integrations.
Menu visibility is a usability projection and cannot grant authority.

## Where each check runs

Every surface below calls the same shared checks before it answers or accepts work. A new
surface must call them too: a route nobody classified is refused (A1), and the tests named
here fail when a surface stops calling them. `TestStage4931ContractNamesLiveCode` fails when
a row names a function or test that no longer exists.

| Row | Surface | Decided by | Proved by |
|---|---|---|---|
| A1 | Every API route: capability and role | `checkRouteCapability` in `apiMiddleware`; an unclassified route is refused | `TestAuthorizationContractMatrix`, `TestUnclassifiedRouteFailsClosed`, `TestStage49GeneratedAuthorizationCases` |
| A2 | Client | `GetTenantSchema`, `SetSearchPath` per transaction; a job runs for its own client (`bindJobTenant`) | `TestStage4932NoSilentDefaultClient`, `TestStage4932JobActsForTheClientItIsQueuedFor` |
| A3 | Read one record | `RoleMayOnDoctype`, then `sessionReadRule` (`ResolveSessionScope`, `DocumentMatchesScope`) | `TestStage73D20DocumentReadRuleVisible`, `TestStage4938OwnerSelfLocationCasesAnswerNotFound` |
| A4 | List, search and count | `ScopeConstraints` in the list query, which the count shares; find by number runs the record's own read (`docReadable`) | `TestStage4934TwoClientNegativeMatrix`, `TestStage139DocFindAnswersOnlyWhatTheSessionCanOpen`, `TestStage4934ListResultsAreNeverCached` |
| A5 | Fields | `FilterFieldsForSession` and `HiddenFieldsForRole` on read, `FilterFieldMetaForRole` on the form, `RejectRestrictedFieldWrites` on write | `TestStage4934HiddenFieldsStayOffEveryReadPath`, `TestStage4938EveryRoleAndSensitiveFieldIsRegistered` |
| A6 | Create and change, workflow state | `LocationOutOfScope`, `selfScopeWriteAllowed`, `isActionOnlyStatus`, then the approval engine | `TestStage4938aCreateNamingAnotherStoreIsRefused`, `TestStage4938StateCasesRefuseGenericSave`, `TestStage4935SubmitterMayNotDecideAnyRoundTheyMade` |
| A7 | Attachments | the record's own read rule (`sessionReadRule`) | `TestStage4934TwoClientNegativeMatrix` |
| A8 | Reports and exports | `RunReportWithControlTotals` masks by role, `ReportStoreScopeFor` cuts by store, `ReportExportJobVisible` keeps an export its owner's | `TestStage7533ReportStoreScope`, `TestStage4982SensitiveColumnsNeverReachAnExport`, `TestStage4982ExportLinkIsOwnerBoundShortLivedAndRevocable`, `TestStage4938ExportRoutesRefuseWrongTenantAndWrongRole`, `TestStage4934PayrollExportTakesTheStoreCut` |
| A9 | Print | the record's read rule, then `RedactPrintDocument` | `TestStage4934PrintByIdRefusesAnotherStoresDocument` |
| A10 | Imports | `RoleMayOnDoctype` and `LocationOutOfScope` per row | `TestStage4934ImportRowsNeedTheRoleToAddOrChange`, `TestStage4934ImportUpdateCannotTakeOverAnotherStoresRecord`, `TestStage4935ImportUpdateMeetsTransitionRules` |
| A11 | Background jobs: list, retry, cancel | capabilities `jobs.view` (Admin, Super Admin, Auditor) and `jobs.manage` (Admin, Super Admin) | `TestStage4931JobRoutesNeedTheirCapability`, `TestStage49103BackgroundExportAndJobsStayInTheirClient` |
| A12 | Error disclosure | `refuseAsNotFound`: another client's or store's record answers like a missing one | `TestStage4938OwnerSelfLocationCasesAnswerNotFound`, `TestStage4934TwoClientNegativeMatrix` |

Use named human and machine identities, minimum required grants and current identity state.
Separate initiation from approval for controlled events under the tenant-approved policy.
Do not recommend two identities for one person to manufacture apparent independence.
Price/refund, beneficiary/payment, payroll, stock write-off and security/audit administration
need explicitly reviewed capabilities, limits, reasons and escalation.

The [role-template engine](../../engines/role_templates.go),
[scope policy](../../engines/scope_policy.go), [sensitive fields](../../engines/sensitive_fields.go)
and [governance handlers](../../internal/server/handlers_access_governance.go) provide current
implementation references. Preview migration against the tenant's own grants and preserve
approved custom roles. Tenant owners must review before/after evidence before activation;
the fact that templates exist does not mean a tenant was migrated.

[Historical permission matrix](../archive/legacy/permission-matrix.md) is a default-tenant snapshot,
not this normative policy or effective authorization. New permission evidence exports
require explicit environment, schema and output root; they remain dated records. Field,
route and workflow controls must also be tested before accepting migration evidence.

The [security threat model](threat-model.md) and [risk register](risk-register.md) own threats
and treatment. This policy provides no blanket security/privacy/compliance certification.

## Approval of the baseline role templates

The product owner approved the policy and the baseline role templates in [role_templates.go](../../engines/role_templates.go) on 2026-11-15 (ask 193, item 48.6.2). The approved baseline is the 16 templates the engine defines: Administrator (Super Admin), Admin, Store Supervisor, Cashier, Picker, Warehouse Manager, Accounts Payable, Accounts Receivable, Accountant, HR Manager, Employee Self-Service, Auditor, Integrator, Setup Operator, Privacy Approver and Merchandiser. A tenant still previews the migration against its own grants and keeps approved custom roles. Tenant matrices are dated assurance records, not this policy.
