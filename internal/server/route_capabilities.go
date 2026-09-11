package server

import (
	"net/http"
	"sync"

	"custom_erp/engines"
)

// Stage 47.1.1 - route-capability registry (audit findings A-01/A-09,
// docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md, "Required closure").
//
// Before this file, apiMiddleware authenticated every request (resolved
// tenant/user/role from the bearer token) but never checked whether the
// resolved ROLE was allowed to call the specific route it authenticated
// for - authorization was left to "enforced in each handler", and A-01
// proved that promise wasn't kept: a signed Cashier token got HTTP 200 from
// the trial balance, the audit log and the system log (see
// stage47_a01_authz_redteam_test.go, which this file is built to flip from
// red to green).
//
// routeCapabilities classifies every route registered through apiMiddleware
// in routes.go: RouteAccessLevel is the coarse tier (public/authenticated/
// admin), Capability is the business concept the route belongs to. The
// completeness test (route_capabilities_test.go) parses routes.go itself
// and fails the build if any apiMiddleware-wrapped route has no entry here -
// so a new route ships classified or the build breaks, per this item's own
// acceptance text ("Registration/test must fail for an authenticated route
// with no classification").
//
// The map below was bootstrapped mechanically (moduleGate name, or the
// first /api/v1/ path segment, or /admin/<segment> for admin routes) rather
// than hand-typed per route - 454 routes make individual review of every
// entry impractical in one pass, and a systematic rule is more auditable
// than 454 separate judgment calls. LevelAdmin is reused for every
// /api/v1/admin/* route (matching routes.go's own repeated "HR/Admin-only,
// enforced in each handler" comments - now actually enforced in one place
// instead of trusted per handler) - this changes no legitimate behavior,
// since none of those routes ever had a non-admin caller. Real enforcement
// beyond "authenticated" is added deliberately and narrowly for now, only
// for the categories 47.1.2 names explicitly (financial statements/
// registers, audit logs, system logs, assets, payroll/HR) via
// capabilityRoleAllowlist below - every other Authenticated-level route
// keeps exactly today's behavior (any signed-in role). Loosening/tightening
// the rest by real task (47.1.5's "safe role templates... from actual
// tasks") is deliberately follow-up work, not guessed here.
type RouteAccessLevel int

const (
	LevelPublic RouteAccessLevel = iota
	LevelAuthenticated
	LevelAdmin
	// LevelBackground is reserved for a route reachable only by this
	// process's own workers/webhooks, never a human session - unused today
	// (nothing in routes.go's apiMiddleware-wrapped surface qualifies; the
	// curated integration surface in routes_public_api_v1.go already has
	// its own separate, purpose-built scope system - see that file).
	LevelBackground
)

type RouteClassification struct {
	Level      RouteAccessLevel
	Capability string
}

// routeCapabilities is keyed by the exact pattern string passed to
// http.HandleFunc in routes.go (e.g. "POST /api/v1/checkout", or a bare
// "/api/v1/logs/audit" for a route registered with no method prefix, which
// Go's ServeMux matches against every method). checkRouteCapability recovers
// the matched pattern for an in-flight request via
// http.DefaultServeMux.Handler(r) - the same (Handler, pattern) lookup the
// mux itself already does to dispatch - so this file needs no change to any
// of routes.go's ~450 registration lines.
//
// Intentionally NOT covered here (never reach this map's lookup):
//   - GET /internal/tls-ask - registered without apiMiddleware at all (Caddy's
//     on_demand_tls ask hook, mid-TLS-handshake, no bearer token possible);
//     already Public in effect, just via a different mechanism.
//   - The SPA shell / static file routes (routes.go's spaShell/http.FileServer
//     registrations) - serve static content only, no apiMiddleware, no data.
//   - registerPublicAPIV1Routes()'s surface (routes_public_api_v1.go) - a
//     deliberately separate integration-credential auth model with its own
//     mandatory per-route scope declaration (publicAPIMiddleware panics at
//     registration for an unscoped route) - already closes this file's own
//     "must classify or fail" requirement for that surface, independently.
var routeCapabilities = map[string]RouteClassification{
	"/api/v1/approval/rules":         {LevelAuthenticated, "approval.default"},
	"/api/v1/crm/loyalty-tier-rules": {LevelAuthenticated, "crm_loyalty.default"},
	"/api/v1/debug/panic":            {LevelAuthenticated, "debug.default"},
	"/api/v1/doc/{doctype}":          {LevelAuthenticated, "doc.default"},
	"/api/v1/doc/{doctype}/{id}":     {LevelAuthenticated, "doc.default"},
	"/api/v1/labels":                 {LevelAuthenticated, "labels.default"},
	// Stage 47.1.5: these two moved from LevelAdmin to capability-gated.
	// LevelAdmin is Super-Admin-only by construction, which made the
	// Auditor template's audit.logs/system.logs capabilities unreachable -
	// the role whose entire job is reading these logs could not read them.
	// Capability gating is the stricter statement anyway (an explicit
	// allowlist, still denying every other role including Cashier, which is
	// what A-01 tests), and it keeps one mechanism instead of two.
	"/api/v1/logs/audit":                                            {LevelAuthenticated, "audit.logs"},
	"/api/v1/logs/system":                                           {LevelAuthenticated, "system.logs"},
	"/api/v1/prefix":                                                {LevelAuthenticated, "prefix.default"},
	"/api/v1/sequence":                                              {LevelAuthenticated, "sequence.default"},
	"DELETE /api/v1/admin/api-credentials/{id}":                     {LevelAdmin, "admin.api-credentials"},
	"DELETE /api/v1/admin/extension/hooks/{id}":                     {LevelAdmin, "admin.extension"},
	"DELETE /api/v1/admin/sandbox-tenants/{id}":                     {LevelAdmin, "admin.sandbox-tenants"},
	"DELETE /api/v1/dashboards/layouts/{id}":                        {LevelAuthenticated, "reports.default"},
	"DELETE /api/v1/meta/{doctype}/fields/{id}":                     {LevelAuthenticated, "meta.default"},
	"DELETE /api/v1/oms/views/{id}":                                 {LevelAuthenticated, "oms.default"},
	"GET /api/v1/admin/api-credentials":                             {LevelAdmin, "admin.api-credentials"},
	"GET /api/v1/admin/api-credentials/{id}/traffic":                {LevelAdmin, "admin.api-credentials"},
	"GET /api/v1/admin/api-traffic":                                 {LevelAdmin, "admin.api-traffic"},
	"GET /api/v1/admin/audit-logs/evidence":                         {LevelAdmin, "admin.audit-logs"},
	"POST /api/v1/admin/audit-logs/checkpoint":                      {LevelAdmin, "admin.audit-logs"},
	"GET /api/v1/admin/audit-logs/verify":                           {LevelAdmin, "admin.audit-logs"},
	"GET /api/v1/admin/extension/hooks":                             {LevelAdmin, "admin.extension"},
	"GET /api/v1/admin/extension/hooks/{id}/log":                    {LevelAdmin, "admin.extension"},
	"GET /api/v1/admin/industries":                                  {LevelAdmin, "admin.industries"},
	"GET /api/v1/admin/modules":                                     {LevelAdmin, "admin.modules"},
	"GET /api/v1/admin/packages":                                    {LevelAdmin, "admin.packages"},
	"GET /api/v1/admin/patch/proposals":                             {LevelAdmin, "admin.patch"},
	"GET /api/v1/admin/public-api/openapi.json":                     {LevelAdmin, "admin.public-api"},
	"GET /api/v1/admin/access-preview":                              {LevelAdmin, "admin.access-preview"},
	"GET /api/v1/admin/role-permissions":                            {LevelAdmin, "admin.role-permissions"},
	"GET /api/v1/admin/role-templates":                              {LevelAdmin, "admin.role-templates"},
	"GET /api/v1/admin/role-templates/migrations":                   {LevelAdmin, "admin.role-templates"},
	"GET /api/v1/admin/role-templates/plan":                         {LevelAdmin, "admin.role-templates"},
	"GET /api/v1/admin/roles":                                       {LevelAdmin, "admin.roles"},
	"GET /api/v1/admin/sod-conflicts":                               {LevelAdmin, "admin.sod-conflicts"},
	"POST /api/v1/admin/role-templates/apply":                       {LevelAdmin, "admin.role-templates"},
	"POST /api/v1/admin/role-templates/revert":                      {LevelAdmin, "admin.role-templates"},
	"GET /api/v1/admin/sandbox-tenants":                             {LevelAdmin, "admin.sandbox-tenants"},
	"GET /api/v1/admin/settings":                                    {LevelAdmin, "admin.settings"},
	"GET /api/v1/admin/tenant-usage":                                {LevelAdmin, "admin.tenant-usage"},
	"GET /api/v1/admin/tenant/module-entitlements":                  {LevelAdmin, "admin.tenant"},
	"GET /api/v1/admin/tenant/version":                              {LevelAdmin, "admin.tenant"},
	"GET /api/v1/admin/tenants":                                     {LevelAdmin, "admin.tenants"},
	"GET /api/v1/admin/users":                                       {LevelAdmin, "admin.users"},
	"GET /api/v1/approval/log":                                      {LevelAuthenticated, "approval.default"},
	"GET /api/v1/approval/pending":                                  {LevelAuthenticated, "approval.default"},
	"GET /api/v1/assets/register":                                   {LevelAuthenticated, "assets.manage"},
	"GET /api/v1/availability":                                      {LevelAuthenticated, "availability.default"},
	"GET /api/v1/clevertap/credentials":                             {LevelAuthenticated, "clevertap.default"},
	"GET /api/v1/clevertap/logs":                                    {LevelAuthenticated, "clevertap.default"},
	"GET /api/v1/dashboards/layouts":                                {LevelAuthenticated, "reports.default"},
	"GET /api/v1/doc/{doctype}/meta":                                {LevelAuthenticated, "doc.default"},
	"GET /api/v1/finance/fx-revaluation":                            {LevelAuthenticated, "finance.default"},
	"GET /api/v1/finance/payment-proposal/{id}/payment-file":        {LevelAuthenticated, "finance.default"},
	"GET /api/v1/finance/payment-proposal/{id}/utrs":                {LevelAuthenticated, "finance.default"},
	"GET /api/v1/finance/periods":                                   {LevelAuthenticated, "finance.default"},
	"GET /api/v1/finance/periods/{id}/close-checklist":              {LevelAuthenticated, "finance.default"},
	"GET /api/v1/finance/trial-balance":                             {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/finance/trial-balance/presentation":                {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/health":                                            {LevelPublic, "health.default"},
	"GET /api/v1/help/article/{slug}":                               {LevelAuthenticated, "help.default"},
	"GET /api/v1/help/index":                                        {LevelAuthenticated, "help.default"},
	"GET /api/v1/help/public/article":                               {LevelPublic, "help.default"},
	"GET /api/v1/help/public/index":                                 {LevelPublic, "help.default"},
	"GET /api/v1/help/public/search-index":                          {LevelPublic, "help.default"},
	"GET /api/v1/help/search-index":                                 {LevelAuthenticated, "help.default"},
	"GET /api/v1/hr/my-employee":                                    {LevelAuthenticated, "hr.default"},
	"GET /api/v1/hr/payroll-export":                                 {LevelAuthenticated, "hr.payroll"},
	"GET /api/v1/hr/salary-components":                              {LevelAuthenticated, "hr.payroll"},
	"GET /api/v1/import/{doctype}/template":                         {LevelAuthenticated, "import.default"},
	"GET /api/v1/integration/logs":                                  {LevelAuthenticated, "integration.default"},
	"GET /api/v1/jobs":                                              {LevelAuthenticated, "jobs.default"},
	"GET /api/v1/localization":                                      {LevelAuthenticated, "localization.default"},
	"GET /api/v1/loyalty/ledger":                                    {LevelAuthenticated, "crm_loyalty.default"},
	"GET /api/v1/manufacturing/active-bom":                          {LevelAuthenticated, "manufacturing.default"},
	"GET /api/v1/manufacturing/mrp-suggestions":                     {LevelAuthenticated, "manufacturing.default"},
	"GET /api/v1/manufacturing/production-schedule":                 {LevelAuthenticated, "manufacturing.default"},
	"GET /api/v1/marketplace/channels/{channel}/credentials":        {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/connectors":                            {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/connectors/health":                     {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/couriers/rates":                        {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/couriers/{provider}/credentials":       {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/logistics/label":                       {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/logistics/label.pdf":                   {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/logistics/serviceability":              {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/sku-exceptions":                        {LevelAuthenticated, "oms.default"},
	"GET /api/v1/marketplace/sku-mappings":                          {LevelAuthenticated, "oms.default"},
	"GET /api/v1/me":                                                {LevelAuthenticated, "me.default"},
	"GET /api/v1/me/mfa/recovery-codes":                             {LevelAuthenticated, "me.default"},
	"GET /api/v1/me/modules":                                        {LevelAuthenticated, "me.default"},
	"GET /api/v1/me/permissions":                                    {LevelAuthenticated, "me.default"},
	"GET /api/v1/meta/doctypes":                                     {LevelAuthenticated, "meta.default"},
	"GET /api/v1/meta/field-formats":                                {LevelAuthenticated, "meta.default"},
	"GET /api/v1/oms/bundles/{sku}/availability":                    {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/gate-passes":                                   {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/orders":                                        {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/orders/search":                                 {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/orders/{id}":                                   {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/pick-queue":                                    {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/shipping-packages":                             {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/tiles":                                         {LevelAuthenticated, "oms.default"},
	"GET /api/v1/oms/views":                                         {LevelAuthenticated, "oms.default"},
	"GET /api/v1/ops/backup-status":                                 {LevelAuthenticated, "ops.default"},
	"GET /api/v1/ops/deployment-status":                             {LevelAuthenticated, "ops.default"},
	"GET /api/v1/optimization/replenishment-suggestions":            {LevelAuthenticated, "optimization.default"},
	"GET /api/v1/optimization/sla-breaches":                         {LevelAuthenticated, "optimization.default"},
	"GET /api/v1/pim/assignable-users":                              {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/catalog-share":                                 {LevelPublic, "pim.default"},
	"GET /api/v1/pim/completeness/{itemCode}":                       {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/content-assist-shapes":                         {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/content-assist/{itemCode}":                     {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/content/{id}/versions":                         {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/dashboard":                                     {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/import-jobs/{id}/errors.csv":                   {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/import-schedules":                              {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/import-templates":                              {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/import-templates/{id}/preview-mapping":         {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media":                                         {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media/bulk-download":                           {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media/search":                                  {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media/transform-presets":                       {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media/{id}/file":                               {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media/{id}/thumbnail":                          {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/media/{id}/transform/{preset}":                 {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/product-groups/{id}/export.csv":                {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/product-groups/{id}/members":                   {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/publish-log":                                   {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/publish-preview":                               {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/publish/{jobID}":                               {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/related-products/{itemCode}":                   {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/reports/{name}":                                {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/search-feed.csv":                               {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/task-templates":                                {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/tasks":                                         {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/taxonomy-history/{doctype}/{id}":               {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/transform-rules":                               {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/workbench":                                     {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/workflow-runs":                                 {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pim/workflows":                                     {LevelAuthenticated, "pim.default"},
	"GET /api/v1/pinelabs/credentials":                              {LevelAuthenticated, "pinelabs.default"},
	"GET /api/v1/pinelabs/transactions":                             {LevelAuthenticated, "pinelabs.default"},
	"GET /api/v1/pos/session/current":                               {LevelAuthenticated, "pos.default"},
	"GET /api/v1/print/qz/certificate":                              {LevelAuthenticated, "stickers.default"},
	"GET /api/v1/print/qz/log":                                      {LevelAuthenticated, "stickers.default"},
	"GET /api/v1/print/qz/printers":                                 {LevelAuthenticated, "stickers.default"},
	"GET /api/v1/procurement/purchase-order/{id}/print":             {LevelAuthenticated, "procurement.default"},
	"GET /api/v1/reports/catalog":                                   {LevelAuthenticated, "reports.default"},
	"GET /api/v1/reports/current-stock":                             {LevelAuthenticated, "reports.default"},
	"GET /api/v1/reports/drilldown/{id}":                            {LevelAuthenticated, "reports.default"},
	"GET /api/v1/reports/export/{id}":                               {LevelAuthenticated, "reports.default"},
	"GET /api/v1/reports/gst-return-summary":                        {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/reports/payables-ageing":                           {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/reports/receivables-ageing":                        {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/reports/run/{id}":                                  {LevelAuthenticated, "reports.default"},
	"GET /api/v1/reports/sales-register":                            {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/reports/vendor-ledger":                             {LevelAuthenticated, "finance.statements"},
	"GET /api/v1/rfq/quotes":                                        {LevelAuthenticated, "rfq.default"},
	"GET /api/v1/setup/status":                                      {LevelAuthenticated, "setup.default"},
	"GET /api/v1/stickers/history":                                  {LevelAuthenticated, "stickers.default"},
	"GET /api/v1/system/environment":                                {LevelAuthenticated, "system.default"},
	"GET /api/v1/unicommerce/credentials":                           {LevelAuthenticated, "oms.default"},
	"GET /api/v1/unicommerce/inventory-syncs":                       {LevelAuthenticated, "oms.default"},
	"GET /api/v1/unicommerce/orders":                                {LevelAuthenticated, "oms.default"},
	"GET /api/v1/version":                                           {LevelPublic, "version.default"},
	"GET /api/v1/wms/batch/allocation-preview":                      {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/billing/charges":                               {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/billing/storage":                               {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/bin-replenishment/demand-driven":               {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/bin-replenishment/dynamic-pickface":            {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/bin-replenishment/suggestions":                 {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/bin-replenishment/wave":                        {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/cockpit":                                       {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/cycle-count/abc-plan":                          {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/cycle-count/plan":                              {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/facility/children":                             {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/facility/descendants":                          {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/labor/plan":                                    {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/loading/bol":                                   {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/lpn/contents":                                  {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/pack-template/resolve":                         {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/packing-validation":                            {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/pick-list":                                     {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/putaway/suggest-bin":                           {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/sortation/slots":                               {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/wave/monitor":                                  {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/wave/pick-list":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/admin/api-credentials":                            {LevelAdmin, "admin.api-credentials"},
	"POST /api/v1/admin/api-credentials/{id}/rotate":                {LevelAdmin, "admin.api-credentials"},
	"POST /api/v1/admin/extension/hooks":                            {LevelAdmin, "admin.extension"},
	"POST /api/v1/admin/extension/token":                            {LevelAdmin, "admin.extension"},
	"POST /api/v1/admin/industry":                                   {LevelAdmin, "admin.industry"},
	"POST /api/v1/admin/patch/approve":                              {LevelAdmin, "admin.patch"},
	"POST /api/v1/admin/patch/reject":                               {LevelAdmin, "admin.patch"},
	"POST /api/v1/admin/role-permissions":                           {LevelAdmin, "admin.role-permissions"},
	"POST /api/v1/admin/sandbox-tenants":                            {LevelAdmin, "admin.sandbox-tenants"},
	"POST /api/v1/admin/sandbox-tenants/{id}/reset":                 {LevelAdmin, "admin.sandbox-tenants"},
	"POST /api/v1/admin/scale-test":                                 {LevelAdmin, "admin.scale-test"},
	"POST /api/v1/admin/tenant/feature-flag":                        {LevelAdmin, "admin.tenant"},
	"POST /api/v1/admin/tenant/host-slug":                           {LevelAdmin, "admin.tenant"},
	"POST /api/v1/admin/tenant/module-entitlement":                  {LevelAdmin, "admin.tenant"},
	"POST /api/v1/admin/tenant/package":                             {LevelAdmin, "admin.tenant"},
	"POST /api/v1/admin/tenant/provision":                           {LevelAdmin, "admin.tenant"},
	"POST /api/v1/admin/users":                                      {LevelAdmin, "admin.users"},
	"POST /api/v1/admin/users/location":                             {LevelAdmin, "admin.users"},
	"POST /api/v1/admin/users/reset-mfa":                            {LevelAdmin, "admin.users"},
	"POST /api/v1/admin/users/reset-password":                       {LevelAdmin, "admin.users"},
	"POST /api/v1/admin/users/status":                               {LevelAdmin, "admin.users"},
	"POST /api/v1/admin/users/supplier":                             {LevelAdmin, "admin.users"},
	"POST /api/v1/approval/bulk-decide":                             {LevelAuthenticated, "approval.default"},
	"POST /api/v1/approval/decide":                                  {LevelAuthenticated, "approval.default"},
	"POST /api/v1/approval/submit":                                  {LevelAuthenticated, "approval.default"},
	"POST /api/v1/assets/capitalize":                                {LevelAuthenticated, "assets.manage"},
	"POST /api/v1/assets/dispose":                                   {LevelAuthenticated, "assets.manage"},
	"POST /api/v1/assets/transfer":                                  {LevelAuthenticated, "assets.manage"},
	"POST /api/v1/auth/forgot-password":                             {LevelPublic, "auth.default"},
	"POST /api/v1/auth/mfa/activate":                                {LevelAuthenticated, "auth.default"},
	"POST /api/v1/auth/mfa/enroll":                                  {LevelAuthenticated, "auth.default"},
	"POST /api/v1/auth/mfa/verify":                                  {LevelAuthenticated, "auth.default"},
	"POST /api/v1/auth/reset-password":                              {LevelPublic, "auth.default"},
	"POST /api/v1/checkout":                                         {LevelAuthenticated, "checkout.default"},
	"POST /api/v1/clevertap/credentials":                            {LevelAuthenticated, "clevertap.default"},
	"POST /api/v1/crm/customer/merge":                               {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/crm/loyalty-redemption/initiate":                  {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/crm/loyalty-redemption/verify":                    {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/crm/voucher/redeem":                               {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/crm/voucher/validate":                             {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/dashboards/layouts":                               {LevelAuthenticated, "reports.default"},
	"POST /api/v1/doc/{doctype}/{id}/reactivate":                    {LevelAuthenticated, "doc.default"},
	"POST /api/v1/expenses/pay":                                     {LevelAuthenticated, "expenses.manage"},
	"POST /api/v1/expenses/verify":                                  {LevelAuthenticated, "expenses.manage"},
	"POST /api/v1/finance/bank-reconcile":                           {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/credit-note/{id}/post":                    {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/debit-note/{id}/post":                     {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/fx-revaluation":                           {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/intercompany-transaction":                 {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/intercompany-transaction/{id}/retry-post": {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/journal-voucher":                          {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/journal-voucher/recurring":                {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/journal-voucher/{id}/retry-post":          {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/journal-voucher/{id}/reverse":             {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/landed-cost-voucher":                      {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/landed-cost-voucher/{id}/apply":           {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/payment-proposal":                         {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/payment-proposal/{id}/execute":            {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/payment-proposal/{id}/record-utr":         {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/periods":                                  {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/periods/{id}/close":                       {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/sales-invoice/{id}/post":                  {LevelAuthenticated, "finance.default"},
	"POST /api/v1/finance/sales-invoice/{id}/settle":                {LevelAuthenticated, "finance.default"},
	"POST /api/v1/fulfillment/return":                               {LevelAuthenticated, "wms.default"},
	"POST /api/v1/fulfillment/task/transition":                      {LevelAuthenticated, "wms.default"},
	"POST /api/v1/gst/calculate":                                    {LevelAuthenticated, "gst.default"},
	"POST /api/v1/hr/disburse-loan":                                 {LevelAuthenticated, "hr.payroll"},
	"POST /api/v1/hr/post-payslip":                                  {LevelAuthenticated, "hr.payroll"},
	"POST /api/v1/hr/run-payroll":                                   {LevelAuthenticated, "hr.payroll"},
	"POST /api/v1/import/{doctype}":                                 {LevelAuthenticated, "import.default"},
	"POST /api/v1/integration/bigcommerce/webhook/{channelCode}":    {LevelAuthenticated, "oms.default"},
	"POST /api/v1/integration/courier/{provider}/tracking":          {LevelAuthenticated, "oms.default"},
	"POST /api/v1/integration/retry":                                {LevelAuthenticated, "integration.default"},
	"POST /api/v1/integration/shopify/order":                        {LevelAuthenticated, "oms.default"},
	"POST /api/v1/integration/shopify/product/map":                  {LevelAuthenticated, "oms.default"},
	"POST /api/v1/integrations/clevertap/segment-sync":              {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/jobs/{id}/cancel":                                 {LevelAuthenticated, "jobs.default"},
	"POST /api/v1/jobs/{id}/retry":                                  {LevelAuthenticated, "jobs.default"},
	"POST /api/v1/login":                                            {LevelPublic, "login.default"},
	"POST /api/v1/loyalty/redeem":                                   {LevelAuthenticated, "crm_loyalty.default"},
	"POST /api/v1/manufacturing/acknowledge-bom-variance":           {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/complete":                           {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/confirm-operation":                  {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/issue-material":                     {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/partial-complete":                   {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/record-actual-cost":                 {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/rework":                             {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/scrap":                              {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/subcontract-order/receive":          {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/manufacturing/subcontract-order/send":             {LevelAuthenticated, "manufacturing.default"},
	"POST /api/v1/marketplace/channels/{channel}/credentials":       {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/channels/{channel}/pull-orders":       {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/channels/{channel}/push-status":       {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/channels/{channel}/sync-inventory":    {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/couriers/{provider}/awb":              {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/couriers/{provider}/cancel":           {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/couriers/{provider}/credentials":      {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/couriers/{provider}/pickup":           {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/logistics/book":                       {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/logistics/manifest":                   {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/logistics/manifest/handover":          {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/logistics/rto":                        {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/logistics/tracking":                   {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/ndr/{id}/resolve":                     {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/settlement/reconcile":                 {LevelAuthenticated, "oms.default"},
	"POST /api/v1/marketplace/sku-mappings":                         {LevelAuthenticated, "oms.default"},
	"POST /api/v1/me/change-password":                               {LevelAuthenticated, "me.default"},
	"POST /api/v1/me/mfa/recovery-codes/regenerate":                 {LevelAuthenticated, "me.default"},
	"POST /api/v1/me/mfa/reenroll":                                  {LevelAuthenticated, "me.default"},
	"POST /api/v1/me/mfa/reenroll/cancel":                           {LevelAuthenticated, "me.default"},
	"POST /api/v1/me/mfa/reenroll/confirm":                          {LevelAuthenticated, "me.default"},
	"POST /api/v1/meta/doctypes":                                    {LevelAuthenticated, "meta.default"},
	"POST /api/v1/meta/{doctype}/fields":                            {LevelAuthenticated, "meta.default"},
	"POST /api/v1/oms/gate-passes":                                  {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/gate-passes/{id}/complete":                    {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/gate-passes/{id}/discard":                     {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/gate-passes/{id}/issue":                       {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/gate-passes/{id}/update":                      {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/orders/bulk":                                  {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/settlements/reconcile":                        {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/settlements/{id}/dispute":                     {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/settlements/{id}/resolve":                     {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/settlements/{id}/write-off":                   {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/shipping-packages":                            {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/shipping-packages/{id}/cancel":                {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/shipping-packages/{id}/invoice":               {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/shipping-packages/{id}/split":                 {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/shipping-packages/{id}/update":                {LevelAuthenticated, "oms.default"},
	"POST /api/v1/oms/views":                                        {LevelAuthenticated, "oms.default"},
	"POST /api/v1/optimization/forecast":                            {LevelAuthenticated, "optimization.default"},
	"POST /api/v1/order-lines/{lineId}/hold":                        {LevelAuthenticated, "oms.default"},
	"POST /api/v1/order-lines/{lineId}/release-hold":                {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders":                                           {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/cancel":                               {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/credit-notes":                         {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/edit":                                 {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/hold":                                 {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/priority":                             {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/release-hold":                         {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/split":                                {LevelAuthenticated, "oms.default"},
	"POST /api/v1/orders/{id}/switch-facility":                      {LevelAuthenticated, "oms.default"},
	"POST /api/v1/pim/barcode/generate":                             {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/bulk-edit":                                    {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/catalogs/{id}/rotate-share-token":             {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/channels/{code}/credentials":                  {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/channels/{code}/pull-state":                   {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/content/{id}/rollback":                        {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/export-templates/{id}/run":                    {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/import-schedules/{id}/rotate-hook-token":      {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/import-templates/{id}/import":                 {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/import-templates/{id}/preview":                {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/import/hook":                                  {LevelPublic, "pim.default"},
	"POST /api/v1/pim/import/{doctype}/preview":                     {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/media/bulk-upload":                            {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/media/upload":                                 {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/media/{id}/deactivate":                        {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/media/{id}/metadata":                          {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/publish":                                      {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/task-templates/{code}/instantiate":            {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/tasks":                                        {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/tasks/bulk":                                   {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/tasks/{id}/{action}":                          {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/translations/seed":                            {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/workflow-runs/bulk":                           {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/workflow-runs/{id}/action":                    {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pim/workflows/{code}/start":                       {LevelAuthenticated, "pim.default"},
	"POST /api/v1/pinelabs/credentials":                             {LevelAuthenticated, "pinelabs.default"},
	"POST /api/v1/pinelabs/reconcile":                               {LevelAuthenticated, "pinelabs.default"},
	"POST /api/v1/pinelabs/transaction":                             {LevelAuthenticated, "pinelabs.default"},
	"POST /api/v1/pos/offers/preview":                               {LevelAuthenticated, "pos.default"},
	"POST /api/v1/pos/quote":                                        {LevelAuthenticated, "pos.default"},
	"POST /api/v1/pos/payment/confirm":                              {LevelAuthenticated, "pos.default"},
	"POST /api/v1/pos/payment/void":                                 {LevelAuthenticated, "pos.default"},
	"POST /api/v1/pos/price-override":                               {LevelAuthenticated, "pos.price_override"},
	"POST /api/v1/pos/offline-heartbeat":                            {LevelAuthenticated, "pos.default"},
	"POST /api/v1/pos/session/close":                                {LevelAuthenticated, "pos.default"},
	"POST /api/v1/pos/session/open":                                 {LevelAuthenticated, "pos.default"},
	"POST /api/v1/print/qz/log":                                     {LevelAuthenticated, "stickers.default"},
	"POST /api/v1/print/qz/payload":                                 {LevelAuthenticated, "stickers.default"},
	"POST /api/v1/print/qz/sign":                                    {LevelAuthenticated, "stickers.default"},
	"POST /api/v1/procurement/convert-requisition":                  {LevelAuthenticated, "procurement.default"},
	"POST /api/v1/procurement/purchase-order/preview":               {LevelAuthenticated, "procurement.default"},
	"POST /api/v1/procurement/purchase-order/{id}/send":             {LevelAuthenticated, "procurement.default"},
	"POST /api/v1/procurement/vendor-invoice/match":                 {LevelAuthenticated, "procurement.default"},
	"POST /api/v1/procurement/vendor-invoice/pay":                   {LevelAuthenticated, "procurement.default"},
	"POST /api/v1/procurement/vendor-invoice/pay-with-tds":          {LevelAuthenticated, "procurement.default"},
	"POST /api/v1/quality/coa":                                      {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/coa/{id}/reject":                          {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/coa/{id}/release":                         {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/maintenance-order/{id}/cancel":            {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/maintenance-order/{id}/complete":          {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/maintenance-order/{id}/start":             {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/ncr":                                      {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/ncr/{id}/close":                           {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/ncr/{id}/investigate":                     {LevelAuthenticated, "quality.default"},
	"POST /api/v1/quality/ncr/{id}/plan-corrective-action":          {LevelAuthenticated, "quality.default"},
	"POST /api/v1/refunds/{id}/approve":                             {LevelAuthenticated, "oms.default"},
	"POST /api/v1/refunds/{id}/process":                             {LevelAuthenticated, "oms.default"},
	"POST /api/v1/refunds/{id}/reject":                              {LevelAuthenticated, "oms.default"},
	"POST /api/v1/reports/export":                                   {LevelAuthenticated, "reports.default"},
	"POST /api/v1/reserve":                                          {LevelAuthenticated, "reserve.default"},
	"GET /api/v1/returns/eligibility":                               {LevelAuthenticated, "oms.default"},
	"POST /api/v1/returns":                                          {LevelAuthenticated, "oms.default"},
	"POST /api/v1/returns/{id}/approve":                             {LevelAuthenticated, "oms.default"},
	"POST /api/v1/returns/{id}/qc":                                  {LevelAuthenticated, "oms.default"},
	"POST /api/v1/returns/{id}/receive":                             {LevelAuthenticated, "oms.default"},
	"POST /api/v1/returns/{id}/reject":                              {LevelAuthenticated, "oms.default"},
	"POST /api/v1/returns/{id}/reverse-pickup":                      {LevelAuthenticated, "oms.default"},
	"POST /api/v1/rfq/select-quote":                                 {LevelAuthenticated, "rfq.default"},
	"POST /api/v1/service/ticket":                                   {LevelAuthenticated, "service.default"},
	"POST /api/v1/service/ticket/{id}/assign":                       {LevelAuthenticated, "service.default"},
	"POST /api/v1/service/ticket/{id}/cancel":                       {LevelAuthenticated, "service.default"},
	"POST /api/v1/service/ticket/{id}/close":                        {LevelAuthenticated, "service.default"},
	"POST /api/v1/service/ticket/{id}/resolve":                      {LevelAuthenticated, "service.default"},
	"POST /api/v1/service/ticket/{id}/start":                        {LevelAuthenticated, "service.default"},
	"POST /api/v1/stickers/print":                                   {LevelAuthenticated, "stickers.default"},
	"POST /api/v1/transfer/dispatch":                                {LevelAuthenticated, "inventory.default"},
	"POST /api/v1/transfer/receive":                                 {LevelAuthenticated, "inventory.default"},
	"POST /api/v1/unicommerce/credentials":                          {LevelAuthenticated, "oms.default"},
	"POST /api/v1/unicommerce/order":                                {LevelAuthenticated, "oms.default"},
	"POST /api/v1/wms/batch/consume":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/batch/expiry-sweep":                           {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/batch/putaway":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/batch/status":                                 {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/billing/charges":                              {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/billing/invoices":                             {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/billing/storage/capture":                      {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/billing/storage/snapshot":                     {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/bin-replenishment/dynamic-pickface/apply":     {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/bin-replenishment/execute":                    {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/bundles/{operation}":                          {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cartonization/suggest":                        {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cartonization/suggest-v2":                     {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/condition-transition":                         {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cross-dock/check":                             {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cross-dock/planned-putaway":                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cross-dock/putaway":                           {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cycle-count/post-adjustment":                  {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cycle-count/reconcile":                        {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cycle-count/recount/request":                  {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cycle-count/recount/submit":                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/cycle-count/variance-reason":                  {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/facility/copy":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/hold/place":                                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/loading/complete":                             {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/loading/create":                               {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/loading/depart":                               {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/loading/scan":                                 {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/lpn/assign":                                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/lpn/deconsolidate":                            {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/owner/assign-warehouse":                       {LevelAuthenticated, "wms.default"},
	"GET /api/v1/wms/owner/mixed-locations":                         {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/owner-stock/assign":                           {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/owner-stock/consume":                          {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/pack-complete":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/pack-scan":                                    {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/physical-inventory/cancel":                    {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/physical-inventory/close":                     {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/physical-inventory/reconcile":                 {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/physical-inventory/start":                     {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/physical-inventory/submit-count":              {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/pick-scan":                                    {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/putaway":                                      {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/robotics/event":                               {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/serial/putaway":                               {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/serial/status":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/short-pick":                                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/sortation/assign":                             {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/sortation/clear":                              {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/sortation/confirm":                            {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/sortation/provision-slots":                    {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/tasks/next":                                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/tasks/transition":                             {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/transfer/pack":                                {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/vas/complete":                                 {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/vas/create":                                   {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/wave/assign":                                  {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/wave/create":                                  {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/wave/template/run":                            {LevelAuthenticated, "wms.default"},
	"POST /api/v1/wms/wave/transition":                              {LevelAuthenticated, "wms.default"},
	"PUT /api/v1/admin/api-credentials/{id}/limits":                 {LevelAdmin, "admin.api-credentials"},
	"PUT /api/v1/admin/settings":                                    {LevelAdmin, "admin.settings"},
	"PUT /api/v1/me":                                                {LevelAuthenticated, "me.default"},
	"PUT /api/v1/meta/{doctype}/fields/{id}":                        {LevelAuthenticated, "meta.default"},
}

// capabilityLookupMux exists only to recover the registered pattern string
// for an in-flight request (Go 1.22's {param}/{param...} matching, the exact
// rules routes.go's real registrations already use). It is NOT
// http.DefaultServeMux: routes.go's Run() - the only code that actually
// registers onto DefaultServeMux - is the server's real entrypoint and is
// never invoked by a unit test (it blocks on ListenAndServe and starts every
// background worker), so DefaultServeMux is empty in every test binary and
// .Handler(r) would report no match for every request, denying everything.
// This mux is built once, lazily, straight from routeCapabilities' own keys
// - the same ~450 pattern strings routes.go registers - with a no-op
// handler, so it is populated identically whether this runs inside the real
// server or a handler test that calls apiMiddleware(handler) directly with
// no server ever started. TestEveryAPIMiddlewareRouteIsClassified/
// TestNoStaleRouteCapabilityEntries (route_capabilities_test.go) are what
// keep this set identical to routes.go's real registrations - this map does
// not re-derive that guarantee, it relies on it.
var (
	capabilityLookupMux     *http.ServeMux
	capabilityLookupMuxOnce sync.Once
)

func matchedRoutePattern(r *http.Request) string {
	capabilityLookupMuxOnce.Do(func() {
		capabilityLookupMux = http.NewServeMux()
		noop := func(http.ResponseWriter, *http.Request) {}
		for pattern := range routeCapabilities {
			capabilityLookupMux.HandleFunc(pattern, noop)
		}
	})
	_, pattern := capabilityLookupMux.Handler(r)
	return pattern
}

// capabilityRoleAllowlist restricts a Capability beyond plain "authenticated"
// - present only for the categories 47.1.2 names explicitly. Absence from
// this map means no restriction beyond the route's RouteAccessLevel (an
// Authenticated-level route stays reachable by any signed-in role, exactly
// today's behavior; an Admin-level route is Super-Admin-only regardless of
// this map, checked separately in checkRouteCapability).
//
// Role choices below (Super Admin, or Super Admin + Store Manager) are
// deliberately conservative given the ACTUAL role model today - only three
// built-in roles exist (engines/roles.go: Super Admin, Store Manager,
// Cashier) plus tenant-defined custom roles this code cannot reason about
// specifically. Store Manager is included only where the frontend already
// treats the capability as open to every role (public/app.js's
// 'menu-finance'/'menu-assets'/'menu-expenses' carry no adminOnly flag,
// unlike 'menu-audit-logs'/'menu-system-status' which already do) - so
// including Store Manager here changes nothing for that role while still
// closing the audit's specific Cashier finding; a custom/unrecognised role
// is denied by default, same fail-closed posture as Cashier. A real,
// task-derived role template set is 47.1.5's job, not guessed here.
// Stage 47.1.5 replaced the hand-written map that used to sit here with one
// derived from the role templates (engines/role_templates.go), so a template
// edit and a route check cannot disagree. restrictedCapabilities names WHICH
// capabilities are enforced beyond "authenticated" - deliberately still just
// the six 47.1.2 identified, because widening enforcement to every capability
// at once would change the behavior of ~450 routes in one step with no
// evidence that the templates' module grants are right for each. The
// templates now decide WHO holds each of the six; extending the list is a
// reviewed, per-capability step.
// Stage 47.2.3 adds pos.price_override as the seventh. It is a new capability
// on a new route, not a widening of an existing one, so it carries none of the
// "changes the behavior of ~450 routes in one step" risk the paragraph above
// is guarding against: before this stage there WAS no price-override command
// - a cashier simply typed a lower price - so every role's access to it is
// being decided for the first time here rather than reduced.
var restrictedCapabilities = []string{
	"audit.logs", "system.logs", "finance.statements",
	"assets.manage", "hr.payroll", "expenses.manage",
	"pos.price_override",
}

var capabilityRoleAllowlist = func() map[string][]string {
	out := make(map[string][]string, len(restrictedCapabilities))
	for _, capability := range restrictedCapabilities {
		// RolesWithCapability returns canonical template names AND their
		// legacy names, so a stored "Store Manager"/"HR/Admin" role still
		// resolves through the template that governs it.
		out[capability] = engines.RolesWithCapability(capability)
	}
	return out
}()

// checkRouteCapability is called from apiMiddleware once role is resolved.
// ok=false means the caller is not authorized; msg names why, for the
// GLOBAL-0011 "Permission denied" response's audit log line.
func checkRouteCapability(pattern string, role string) (ok bool, classification RouteClassification, found bool) {
	classification, found = routeCapabilities[pattern]
	if !found {
		// No registered classification for a route that reached this check
		// (i.e. it went through apiMiddleware and past the publicRoutes
		// gate) - fail closed rather than silently defaulting to open. The
		// completeness test (route_capabilities_test.go) is what should
		// catch this at build time; reaching it at request time means that
		// guard was bypassed or the two fell out of sync some other way.
		return false, classification, false
	}
	switch classification.Level {
	case LevelPublic:
		return true, classification, true
	case LevelAdmin:
		return engines.IsSuperAdmin(role), classification, true
	default: // LevelAuthenticated, LevelBackground
		if allowed, restricted := capabilityRoleAllowlist[classification.Capability]; restricted {
			for _, want := range allowed {
				// engines.RoleSuperAdmin in an allowlist means "either
				// spelling" (IsSuperAdmin accepts the legacy "HR/Admin"
				// name too, see roles.go) - a plain string == would wrongly
				// deny a legacy-named Super Admin session.
				if want == engines.RoleSuperAdmin {
					if engines.IsSuperAdmin(role) {
						return true, classification, true
					}
					continue
				}
				if want == role {
					return true, classification, true
				}
			}
			return false, classification, true
		}
		return true, classification, true
	}
}
