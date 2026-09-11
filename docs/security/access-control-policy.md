---
doc_id: SEC-ACCESS-001
title: Access and segregation-of-duties policy
type: normative
status: draft
owner: security-owner
approvers: [security-owner, finance-process-owner, hr-process-owner, tenant-owner]
audience: [tenant-admins, security, process-owners, engineering]
applies_to: tenant-approved capability, field and scope policy
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Access and segregation-of-duties policy

Evaluate route capability, object/action, tenant, entity/location/owner/self scope, field
classification and workflow state server-side before returning data or accepting work.
Apply the same policy to search/count/export, attachments, jobs, imports and integrations.
Menu visibility is a usability projection and cannot grant authority.

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

[Historical permission matrix](../guides/PERMISSION_MATRIX.md) is a default-tenant snapshot,
not this normative policy or effective authorization. New permission evidence exports
require explicit environment, schema and output root; they remain dated records. Field,
route and workflow controls must also be tested before accepting migration evidence.

The [security threat model](threat_model.md) and [risk register](risk_register.md) own threats
and treatment. This draft provides no blanket security/privacy/compliance certification.
