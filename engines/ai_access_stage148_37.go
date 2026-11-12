package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Stage 148.37 (decision D9): read-only access for AI assistants, module
// ai_access, off by default.
//
// An assistant is an OAuth client (38.2d) tied, in ai_access_clients, to ONE
// named user. It holds only the scopes ai:read and ai:draft, which the public
// catalog does not offer, so the ordinary credential routes can neither make
// one nor give an ordinary client these scopes. Every call re-reads that
// user's live state with no cache (a disabled user stops the assistant on its
// next call) and works with the user's own role, store scope and field rules.
// Read is enforced here by what exists: the read routes only select, and the
// one write is a Draft sales order that this file can never post, reserve or
// approve - a person submits it (SubmitAssistantDraft) and the ordinary order
// path does the rest.

const (
	AIScopeRead  = "ai:read"
	AIScopeDraft = "ai:draft"
	// AIAccessModule is the module key the routes sit behind.
	AIAccessModule = "ai_access"

	aiMaxDraftLines = 50
	aiMaxDraftQty   = 100000
	aiReportRows    = 200
	// Stage 148.37 review: caps on what an assistant may leave behind.
	aiMaxOpenDrafts   = 20
	aiMaxCustomerName = 200
	aiMaxShipAddress  = 500
)

var (
	ErrAIClientUnbound   = errors.New("this client is not tied to a user")
	ErrAIUserInactive    = errors.New("the user this assistant acts as is not active")
	ErrAIStoreNotAllowed = errors.New("that store is not one of the user's stores")
	ErrAIOrderNotFound   = errors.New("order not found")
	ErrAIReportNotFound  = errors.New("report not found")
	ErrAIReportRefused   = errors.New("the user's role cannot run this report")
	// ErrAIUserFullAccess: an assistant never acts as an Admin or Super Admin.
	ErrAIUserFullAccess = errors.New("the user this assistant acts as has full access")
)

// AIDraftError is a draft order the assistant sent that cannot be accepted.
type AIDraftError struct{ Message string }

func (e *AIDraftError) Error() string { return e.Message }

// AIIssueError is an assistant client that cannot be made.
type AIIssueError struct{ Message string }

func (e *AIIssueError) Error() string { return e.Message }

// AIActor is who a call acts as, resolved fresh on every call.
type AIActor struct {
	TenantID     string
	CredentialID string
	ClientID     string
	UserID       string
	State        LiveUserState
}

// AIAccessClient is one row of the admin list.
type AIAccessClient struct {
	CredentialID string     `json:"credential_id"`
	Name         string     `json:"name"`
	ClientID     string     `json:"client_id"`
	ActingUser   string     `json:"acting_user"`
	UserName     string     `json:"user_name"`
	UserActive   bool       `json:"user_active"`
	Scopes       []string   `json:"scopes"`
	Status       string     `json:"status"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CreatedBy    string     `json:"created_by"`
}

func aiScopesValid(scopes []string) ([]string, error) {
	seen := map[string]bool{}
	for _, raw := range scopes {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if s != AIScopeRead && s != AIScopeDraft {
			return nil, &AIIssueError{Message: fmt.Sprintf("%q is not a choice for an assistant - use ai:read or ai:draft", raw)}
		}
		seen[s] = true
	}
	if len(seen) == 0 {
		return nil, &AIIssueError{Message: "pick at least one of ai:read, ai:draft"}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// IssueAIAccessClient makes an OAuth client that acts as actingUser. The
// secret is returned once, like any other client.
func IssueAIAccessClient(tenantID, name, actingUser string, scopes []string, createdBy string) (*IssuedOAuthClient, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, &AIIssueError{Message: "give the client a name of up to 120 letters"}
	}
	scopes, err := aiScopesValid(scopes)
	if err != nil {
		return nil, err
	}
	actingUser = strings.TrimSpace(actingUser)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var status, role string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(role,'') FROM %s.users WHERE id = $1`, schema), actingUser).Scan(&status, &role)
	if err == sql.ErrNoRows || (err == nil && status != "Active") {
		return nil, &AIIssueError{Message: "the user must be an active user"}
	}
	if err != nil {
		return nil, err
	}
	// The issuer is a full-access admin (the route gate), so the issuer's rights
	// cover any user who is not full access; a full-access user is refused.
	if HasFullAccess(role) {
		return nil, &AIIssueError{Message: "an Admin or Super Admin cannot be an assistant - pick a user with a narrower role"}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	issued, err := insertAPICredential(tx, schema, name, scopes, nil, createdBy, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET auth_mode = 'oauth_client' WHERE id = $1`, schema), issued.Credential.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.ai_access_clients (credential_id, acting_user, created_by) VALUES ($1,$2,$3)`, schema),
		issued.Credential.ID, actingUser, createdBy); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	issued.Credential.AuthMode = "oauth_client"
	return OAuthClientFromIssued(issued), nil
}

// ListAIAccessClients is every assistant client, newest first, with the user
// it acts as and whether that user is still active.
func ListAIAccessClients(tenantID string) ([]AIAccessClient, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT c.id::text, c.name, c.key_prefix, a.acting_user, COALESCE(u.username, ''), COALESCE(u.status = 'Active', FALSE),
		       c.scopes::text, c.status, c.last_used_at, c.created_at, a.created_by
		  FROM %[1]s.ai_access_clients a
		  JOIN %[1]s.api_credentials c ON c.id = a.credential_id
		  LEFT JOIN %[1]s.users u ON u.id = a.acting_user
		 ORDER BY c.created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIAccessClient{}
	for rows.Next() {
		var c AIAccessClient
		var scopes string
		var last sql.NullTime
		if err := rows.Scan(&c.CredentialID, &c.Name, &c.ClientID, &c.ActingUser, &c.UserName, &c.UserActive,
			&scopes, &c.Status, &last, &c.CreatedAt, &c.CreatedBy); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopes), &c.Scopes)
		if last.Valid {
			t := last.Time
			c.LastUsedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResolveAIActor finds the user a client acts as and reads that user fresh. The
// live-state cache is dropped first: a disabled user must stop the assistant on
// its very next call, not up to thirty seconds later.
func ResolveAIActor(tenantID, clientID string) (*AIActor, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var credID, user, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT c.id::text, a.acting_user, c.status
		  FROM %[1]s.api_credentials c JOIN %[1]s.ai_access_clients a ON a.credential_id = c.id
		 WHERE c.key_prefix = $1`, schema), clientID).Scan(&credID, &user, &status)
	if err == sql.ErrNoRows || (err == nil && status != "Active") {
		return nil, ErrAIClientUnbound
	}
	if err != nil {
		return nil, err
	}
	actor := &AIActor{TenantID: tenantID, CredentialID: credID, ClientID: clientID, UserID: user}
	InvalidateLiveUserState(tenantID, user)
	state, err := ResolveLiveUserState(tenantID, user)
	if IsUserNotActiveError(err) {
		return actor, ErrAIUserInactive
	}
	if err != nil {
		return nil, err
	}
	actor.State = state
	// Re-checked on every call: a user promoted after the client was made stops working.
	if HasFullAccess(state.Role) {
		return actor, ErrAIUserFullAccess
	}
	return actor, nil
}

// AuditAICall writes one audit row per call, allowed or refused, naming the
// client and the user it acted as.
func AuditAICall(tenantID string, actor *AIActor, clientID, userID, call, outcome, detail string) {
	if actor != nil {
		clientID, userID = actor.ClientID, actor.UserID
	}
	action := "AI_ACCESS_READ"
	switch {
	case outcome != "allowed":
		action = "AI_ACCESS_DENIED"
	case strings.HasPrefix(call, "POST"):
		action = "AI_ACCESS_DRAFT"
	}
	status := "Success"
	if outcome != "allowed" {
		status = "Failure"
	}
	LogAuditEvent(tenantID, userID, action, status,
		fmt.Sprintf("assistant client %s acting as user %s: %s - %s %s", clientID, userID, call, outcome, detail))
}

// AIStockLevel is the assistant's view of one stock row.
type AIStockLevel struct {
	SKU             string `json:"sku"`
	Location        string `json:"location"`
	AvailableToSell int    `json:"available_to_sell"`
	AsOf            string `json:"as_of,omitempty"`
}

// AIStock is stock of one item at the user's stores. A location the user may
// not work at is refused outright; no location means every store they have.
func AIStock(a *AIActor, sku, location string) ([]AIStockLevel, error) {
	location = strings.TrimSpace(location)
	var levels []PublicInventoryLevel
	switch {
	case location != "":
		if !a.State.Access.Allows(location) {
			return nil, ErrAIStoreNotAllowed
		}
		l, err := ListPublicInventory(a.TenantID, sku, location)
		if err != nil {
			return nil, err
		}
		levels = l
	case a.State.Access.All:
		l, err := ListPublicInventory(a.TenantID, sku, "")
		if err != nil {
			return nil, err
		}
		levels = l
	default:
		for _, store := range a.State.Access.List() {
			l, err := ListPublicInventory(a.TenantID, sku, store)
			if err != nil {
				return nil, err
			}
			levels = append(levels, l...)
		}
	}
	out := make([]AIStockLevel, 0, len(levels))
	for _, l := range levels {
		out = append(out, AIStockLevel{SKU: l.SKU, Location: l.LocationCode, AvailableToSell: l.Available, AsOf: l.AsOf})
	}
	return out, nil
}

// aiOrderVisible: the order is visible when the user has every store, or any of
// its lines (or the order itself, for a draft) sits at one of their stores.
func aiOrderVisible(schema, orderID string, orderData map[string]interface{}, access StoreAccess) bool {
	if access.All {
		return true
	}
	if loc, _ := orderData["location_code"].(string); loc != "" && access.Allows(loc) {
		return true
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT COALESCE(data->>'location_code','')
		FROM %s.documents WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'order_id' = $1`, schema), orderID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var loc string
		if rows.Scan(&loc) == nil && access.Allows(loc) {
			return true
		}
	}
	return false
}

// aiOrderPartsAtStores cuts an order's lines to those at the user's stores and
// its shipments to those booked from them. A shipment is shown when every line
// of the order is at the user's stores, or when the booking itself names one of
// their stores; otherwise it could be another store's parcel.
func aiOrderPartsAtStores(schema, orderID string, access StoreAccess, lines []PublicOrderStatusLine, shipments []PublicOrderShipment) ([]PublicOrderStatusLine, []PublicOrderShipment) {
	keptLines := []PublicOrderStatusLine{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'sku',''), COALESCE(NULLIF(data->>'qty','')::numeric, 0),
		COALESCE(NULLIF(data->>'line_status',''), status), COALESCE(data->>'location_code','')
		FROM %s.documents WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'order_id' = $1 ORDER BY id`, schema), orderID)
	if err != nil {
		return []PublicOrderStatusLine{}, []PublicOrderShipment{}
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var l PublicOrderStatusLine
		var loc string
		if rows.Scan(&l.SKU, &l.Quantity, &l.Status, &loc) != nil {
			continue
		}
		total++
		if access.Allows(loc) {
			keptLines = append(keptLines, l)
		}
	}
	keptShipments := []PublicOrderShipment{}
	if total == len(keptLines) {
		return keptLines, shipments
	}
	srows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'tracking_number',''), COALESCE(data->>'location_code', data->>'origin_location', '')
		FROM %s.documents WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND data->>'order_id' = $1`, schema), orderID)
	if err != nil {
		return keptLines, keptShipments
	}
	defer srows.Close()
	ok := map[string]bool{}
	for srows.Next() {
		var track, loc string
		if srows.Scan(&track, &loc) == nil && access.Allows(loc) {
			ok[track] = true
		}
	}
	for _, s := range shipments {
		if ok[s.TrackingNumber] {
			keptShipments = append(keptShipments, s)
		}
	}
	return keptLines, keptShipments
}

// AIOrderStatus is the order tracking view, for an order at the user's stores.
// An order that does not exist and one the user may not see answer the same.
func AIOrderStatus(a *AIActor, identifier string) (map[string]interface{}, error) {
	status, err := GetPublicOrderStatus(a.TenantID, identifier)
	if errors.Is(err, ErrPublicNotFound) {
		return nil, ErrAIOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(a.TenantID)
	if err != nil {
		return nil, err
	}
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = $1 AND doctype = 'SalesOrder'`, schema), status.OrderID).Scan(&raw); err != nil {
		return nil, ErrAIOrderNotFound
	}
	data := map[string]interface{}{}
	_ = json.Unmarshal([]byte(raw), &data)
	if !aiOrderVisible(schema, status.OrderID, data, a.State.Access) {
		return nil, ErrAIOrderNotFound
	}
	made, _ := data["made_by_assistant"].(bool)
	lines, shipments := status.Lines, status.Shipments
	if !a.State.Access.All {
		lines, shipments = aiOrderPartsAtStores(schema, status.OrderID, a.State.Access, lines, shipments)
	}
	return map[string]interface{}{
		"order_id": status.OrderID, "channel_order_id": status.ChannelOrderID, "channel": status.Channel,
		"status": status.Status, "hold_reason": status.HoldReason, "placed_at": status.PlacedAt, "updated_at": status.UpdatedAt,
		"lines": lines, "shipments": shipments, "made_by_assistant": made,
	}, nil
}

// AIReportList is the reports this user's role may run, in the module's
// catalog order.
func AIReportList(a *AIActor) []map[string]interface{} {
	modules := map[string]bool{}
	out := []map[string]interface{}{}
	for _, d := range ListReportDefinitions() {
		if !ReportAllowedForRole(d, a.State.Role) {
			continue
		}
		ok, seen := modules[d.ModuleKey]
		if !seen {
			ok = RequireModules(a.TenantID, "reports", d.ModuleKey) == nil
			modules[d.ModuleKey] = ok
		}
		if !ok {
			continue
		}
		params := []map[string]interface{}{}
		for _, p := range d.Params {
			params = append(params, map[string]interface{}{"key": p.Key, "required": p.Required})
		}
		out = append(out, map[string]interface{}{"id": d.ID, "label": d.Label, "category": d.Category, "params": params})
	}
	return out
}

// AIReportRun runs one report as the user: their role gate, their store cut,
// their field masking - the same RunReportContext the screens use.
func AIReportRun(ctx context.Context, a *AIActor, reportID string, params map[string]string) (map[string]interface{}, error) {
	def, known := reportRegistry[reportID]
	if !known {
		return nil, ErrAIReportNotFound
	}
	if !ReportAllowedForRole(def, a.State.Role) {
		return nil, ErrAIReportRefused
	}
	page := &ReportPage{Limit: aiReportRows}
	ctx = WithReportPage(ctx, page)
	d, rows, masked, err := RunReportContext(ctx, a.TenantID, reportID, a.State.Role, a.UserID, params)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": d.ID, "label": d.Label, "columns": d.Columns, "rows": rows, "masked": masked,
		"total_rows": page.Total, "row_limit": aiReportRows}, nil
}

// AIDraftLine / AIDraftInput are the whole of what an assistant may say.
type AIDraftLine struct {
	SKU       string  `json:"sku"`
	Qty       int     `json:"qty"`
	UnitPrice float64 `json:"unit_price"`
}

type AIDraftInput struct {
	CustomerName    string        `json:"customer_name"`
	ShippingAddress string        `json:"shipping_address"`
	Lines           []AIDraftLine `json:"lines"`
}

// CreateAssistantDraft saves a Draft sales order. It writes one document and
// nothing else: no stock reservation, no task, no number, no acceptance. The
// record carries who made it. Every field but the three above is the server's.
func CreateAssistantDraft(a *AIActor, in AIDraftInput) (string, error) {
	if len(in.Lines) == 0 || len(in.Lines) > aiMaxDraftLines {
		return "", &AIDraftError{Message: fmt.Sprintf("send between 1 and %d lines", aiMaxDraftLines)}
	}
	in.CustomerName, in.ShippingAddress = strings.TrimSpace(in.CustomerName), strings.TrimSpace(in.ShippingAddress)
	if utf8.RuneCountInString(in.CustomerName) > aiMaxCustomerName {
		return "", &AIDraftError{Message: fmt.Sprintf("the customer name is longer than %d letters", aiMaxCustomerName)}
	}
	if utf8.RuneCountInString(in.ShippingAddress) > aiMaxShipAddress {
		return "", &AIDraftError{Message: fmt.Sprintf("the shipping address is longer than %d letters", aiMaxShipAddress)}
	}
	schema, err := db.GetTenantSchema(a.TenantID)
	if err != nil {
		return "", err
	}
	var open int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
		AND status = 'Draft' AND data->>'made_by_assistant' = 'true' AND data->>'assistant_client' = $1`, schema), a.ClientID).Scan(&open); err != nil {
		return "", err
	}
	if open >= aiMaxOpenDrafts {
		return "", &AIDraftError{Message: fmt.Sprintf("%d drafts are waiting for a person - ask them to submit or clear some first", open)}
	}
	lines := make([]map[string]interface{}, 0, len(in.Lines))
	var total float64
	for _, l := range in.Lines {
		sku := strings.TrimSpace(l.SKU)
		if sku == "" || l.Qty < 1 || l.Qty > aiMaxDraftQty || l.UnitPrice < 0 {
			return "", &AIDraftError{Message: "each line needs an item and a quantity above zero"}
		}
		item, err := ResolveItemBySKU(a.TenantID, sku)
		if err != nil {
			if errors.Is(err, ErrItemNotFound) {
				return "", &AIDraftError{Message: fmt.Sprintf("item %q is not known", sku)}
			}
			return "", err
		}
		if item.Status != "Active" {
			return "", &AIDraftError{Message: fmt.Sprintf("item %q is not active", sku)}
		}
		lines = append(lines, map[string]interface{}{"sku": sku, "qty": l.Qty, "unit_price": l.UnitPrice})
		total += l.UnitPrice * float64(l.Qty)
	}
	id := NewDocID("SO")
	doc := map[string]interface{}{
		"code": id, "channel": "AI assistant", "customer_name": strings.TrimSpace(in.CustomerName),
		"shipping_address": strings.TrimSpace(in.ShippingAddress), "order_status": "Draft",
		"total_amount": total, "lines": lines, "made_by_assistant": true,
		"assistant_user": a.UserID, "assistant_client": a.ClientID,
		"location_code": a.State.Access.Home,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrder', $2, 'Draft', $3)`, schema),
		id, raw, a.UserID); err != nil {
		return "", err
	}
	return id, nil
}

// SubmitAssistantDraft is the person's step: the draft becomes a real order
// through CreateSalesOrder (reservation, holds, acceptance as for any order
// keyed in by staff), and the draft is closed with a link both ways. The user
// must work at the draft's store.
func SubmitAssistantDraft(tenantID, draftID, userID string) (string, error) {
	state, err := ResolveLiveUserState(tenantID, userID)
	if err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents
		WHERE id = $1 AND doctype = 'SalesOrder' AND deleted_at IS NULL AND status = 'Draft'
		  AND data->>'made_by_assistant' = 'true'`, schema), draftID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", ErrAIOrderNotFound
	}
	if err != nil {
		return "", err
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", err
	}
	if !state.Access.All {
		if loc, _ := data["location_code"].(string); loc == "" || !state.Access.Allows(loc) {
			return "", ErrAIStoreNotAllowed
		}
	}
	// Claim it first so two people pressing Submit make one order.
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Closed', data = data || '{"order_status":"Closed"}'::jsonb
		WHERE id = $1 AND doctype = 'SalesOrder' AND status = 'Draft'`, schema), draftID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", &AIDraftError{Message: "this draft was already submitted"}
	}
	release := func() {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Draft', data = data || '{"order_status":"Draft"}'::jsonb
			WHERE id = $1 AND doctype = 'SalesOrder'`, schema), draftID)
	}
	in := SalesOrderInput{
		Channel:         "AI assistant",
		CustomerName:    fmt.Sprint(data["customer_name"]),
		ShippingAddress: fmt.Sprint(data["shipping_address"]),
		SkipPending:     true,
		AcceptedBy:      userID,
	}
	if in.CustomerName == "<nil>" {
		in.CustomerName = ""
	}
	if in.ShippingAddress == "<nil>" {
		in.ShippingAddress = ""
	}
	lines, _ := data["lines"].([]interface{})
	for _, l := range lines {
		m, _ := l.(map[string]interface{})
		sku, _ := m["sku"].(string)
		qty, _ := m["qty"].(float64)
		price, _ := m["unit_price"].(float64)
		in.Lines = append(in.Lines, SalesOrderLineInput{SKU: sku, Qty: int(qty), UnitPrice: price})
	}
	orderID, err := CreateSalesOrder(tenantID, in)
	if err != nil {
		release()
		return "", err
	}
	link, _ := json.Marshal(map[string]interface{}{"submitted_from_draft": draftID, "submitted_by": userID, "made_by_assistant": true,
		"assistant_user": data["assistant_user"], "assistant_client": data["assistant_client"]})
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE id = $1 AND doctype = 'SalesOrder'`, schema), orderID, link)
	closed, _ := json.Marshal(map[string]interface{}{"submitted_order": orderID, "submitted_by": userID})
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE id = $1 AND doctype = 'SalesOrder'`, schema), draftID, closed)
	return orderID, nil
}

// carryAIAccessLinkTx keeps an assistant's user binding across a key rotation:
// the replacement credential acts as the same user, and the old link goes with
// the old (revoked) credential. A tenant that has not got the table yet, or a
// credential that is not an assistant, does nothing.
func carryAIAccessLinkTx(tx *sql.Tx, schema, oldID, newID string) error {
	var present bool
	if err := tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".ai_access_clients").Scan(&present); err != nil || !present {
		return err
	}
	_, err := tx.Exec(fmt.Sprintf(`INSERT INTO %[1]s.ai_access_clients (credential_id, acting_user, created_by)
		SELECT $2::uuid, acting_user, created_by FROM %[1]s.ai_access_clients WHERE credential_id = $1::uuid
		ON CONFLICT DO NOTHING`, schema), oldID, newID)
	return err
}

// AssistantDraftRow is one open assistant draft as a person sees it.
type AssistantDraftRow struct {
	ID        string  `json:"id"`
	Customer  string  `json:"customer_name"`
	Address   string  `json:"shipping_address"`
	Store     string  `json:"location_code"`
	Total     float64 `json:"total_amount"`
	Lines     int     `json:"line_count"`
	Assistant string  `json:"assistant_user"`
	Made      string  `json:"created_at"`
}

// ListAssistantDrafts is the open drafts at the user's stores, oldest first.
// A draft with no store is shown only to a user who works at every store, the
// same rule Submit applies.
func ListAssistantDrafts(tenantID, userID string) ([]AssistantDraftRow, error) {
	state, err := ResolveLiveUserState(tenantID, userID)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text, created_at::text FROM %s.documents
		WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND status = 'Draft'
		  AND data->>'made_by_assistant' = 'true' ORDER BY created_at, id LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssistantDraftRow{}
	for rows.Next() {
		var id, raw, made string
		if err := rows.Scan(&id, &raw, &made); err != nil {
			return nil, err
		}
		data := map[string]interface{}{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		loc, _ := data["location_code"].(string)
		if !state.Access.All && (loc == "" || !state.Access.Allows(loc)) {
			continue
		}
		lines, _ := data["lines"].([]interface{})
		out = append(out, AssistantDraftRow{ID: id, Customer: strField(data, "customer_name"), Address: strField(data, "shipping_address"),
			Store: loc, Total: numFromInterface(data["total_amount"]), Lines: len(lines), Assistant: strField(data, "assistant_user"), Made: made})
	}
	return out, rows.Err()
}
