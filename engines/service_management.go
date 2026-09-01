package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 37.8: Service management. Pre-build audit found no ServiceTicket/
// WorkOrder/AMC concept anywhere. WarehouseTask's dispatch spine (Stage 42.2)
// is WMS-specific (bins/batches/zones) and not directly reusable as an
// object, but its LIFECYCLE PATTERN - a typed status enum, a terminal-state
// guard, reason-required transitions - is what ServiceTicket's own dedicated
// engine functions copy, the same way IntercompanyTransaction/
// LandedCostVoucher/PrepaidExpenseSchedule each enforce their own valid
// transitions explicitly rather than adopting Stage 29.8's opt-in
// StatusTransitionRule map (that governs the GENERIC document API path;
// these doctypes move through dedicated engine functions instead).
// "Technician assignment" reuses WarehouseTask.AssignedTo's own convention:
// a bare username string, no Employee-link enforcement - that object
// doesn't validate one either.

var serviceTicketTerminalStatuses = map[string]bool{"Resolved": true, "Closed": true, "Cancelled": true}

func fetchServiceTicket(schema, ticketID string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'ServiceTicket' AND id = $1`, schema), ticketID).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("service ticket not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("service ticket %s has corrupt stored data: %v", ticketID, err)
	}
	return data, status, nil
}

func updateServiceTicket(schema, ticketID, status string, data map[string]interface{}) error {
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ServiceTicket' AND id = $3`, schema),
		marshaled, status, ticketID)
	return err
}

var serviceTicketPriorities = map[string]bool{"Low": true, "Medium": true, "High": true, "Critical": true}

// CreateServiceTicket creates a Draft ServiceTicket. asset/serviceContractID
// are optional - a ticket for an unregistered asset or with no AMC coverage
// is still a real, valid ticket.
func CreateServiceTicket(tenantID, customer, description, priority, asset, serviceContractID, respondByDate, resolveByDate string, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if customer == "" {
		return "", fmt.Errorf("customer is required")
	}
	if description == "" {
		return "", fmt.Errorf("description is required")
	}
	if priority == "" {
		priority = "Medium"
	}
	if !serviceTicketPriorities[priority] {
		return "", fmt.Errorf("priority must be one of Low, Medium, High, Critical")
	}
	if serviceContractID != "" {
		if _, _, err := fetchDocData(tenantID, "ServiceContract", serviceContractID); err != nil {
			return "", fmt.Errorf("service_contract_id: %v", err)
		}
	}

	id := NewDocID("SVC")
	docData := map[string]interface{}{
		"id": id, "code": id, "customer": customer, "description": description,
		"priority": priority, "asset": asset, "service_contract_id": serviceContractID,
		"respond_by_date": respondByDate, "resolve_by_date": resolveByDate, "status": "Draft",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ServiceTicket', $2, 'Draft', $3)`, schema),
		id, marshaled, userID); err != nil {
		return "", err
	}
	return id, nil
}

// AssignServiceTicket assigns a technician (a bare username, the
// WarehouseTask.AssignedTo convention) and moves Draft -> Assigned. Also
// callable again on an already-Assigned ticket to reassign, since that is
// not a state change.
func AssignServiceTicket(tenantID, ticketID, assignedTo string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if assignedTo == "" {
		return fmt.Errorf("assigned_to is required")
	}
	data, status, err := fetchServiceTicket(schema, ticketID)
	if err != nil {
		return err
	}
	if serviceTicketTerminalStatuses[status] {
		return fmt.Errorf("cannot assign a %s service ticket", status)
	}
	data["assigned_to"] = assignedTo
	newStatus := status
	if status == "Draft" {
		newStatus = "Assigned"
	}
	return updateServiceTicket(schema, ticketID, newStatus, data)
}

// StartServiceTicket moves Assigned -> InProgress - work has actually begun.
func StartServiceTicket(tenantID, ticketID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchServiceTicket(schema, ticketID)
	if err != nil {
		return err
	}
	if status != "Assigned" {
		return fmt.Errorf("only an Assigned service ticket can be started (current status: %s)", status)
	}
	return updateServiceTicket(schema, ticketID, "InProgress", data)
}

// ResolveServiceTicket moves InProgress -> Resolved, requiring resolution
// notes - a real ServiceTicket's whole point is a written record of what
// was done, not just a status flip.
func ResolveServiceTicket(tenantID, ticketID, resolutionNotes string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if resolutionNotes == "" {
		return fmt.Errorf("resolution_notes is required to resolve a service ticket")
	}
	data, status, err := fetchServiceTicket(schema, ticketID)
	if err != nil {
		return err
	}
	if status != "InProgress" {
		return fmt.Errorf("only an InProgress service ticket can be resolved (current status: %s)", status)
	}
	data["resolution_notes"] = resolutionNotes
	return updateServiceTicket(schema, ticketID, "Resolved", data)
}

// CloseServiceTicket moves Resolved -> Closed and, if the ticket references
// a ServiceContract, consumes one visit from it - the entitlement is used
// once the visit is genuinely done, not when it's merely logged.
func CloseServiceTicket(tenantID, ticketID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchServiceTicket(schema, ticketID)
	if err != nil {
		return err
	}
	if status != "Resolved" {
		return fmt.Errorf("only a Resolved service ticket can be closed (current status: %s)", status)
	}
	if contractID, _ := data["service_contract_id"].(string); contractID != "" {
		if err := ConsumeServiceContractVisit(tenantID, contractID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "CloseServiceTicket", fmt.Sprintf("ticket %s closed but its contract %s visit could not be consumed: %v", ticketID, contractID, err), "")
		}
	}
	return updateServiceTicket(schema, ticketID, "Closed", data)
}

// CancelServiceTicket refuses to cancel an already-terminal ticket and
// requires a reason, matching every other cancellation path in this
// codebase (GatePass, WarehouseTask exceptions, ...).
func CancelServiceTicket(tenantID, ticketID, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if reason == "" {
		return fmt.Errorf("cancellation_reason is required")
	}
	data, status, err := fetchServiceTicket(schema, ticketID)
	if err != nil {
		return err
	}
	if serviceTicketTerminalStatuses[status] {
		return fmt.Errorf("service ticket %s is already %s", ticketID, status)
	}
	data["cancellation_reason"] = reason
	return updateServiceTicket(schema, ticketID, "Cancelled", data)
}

// ---------------------------------------------------------------------------
// 37.8.3: ServiceContract (AMC) - entitlement tracking. One asset per
// contract, a stated scope decision (multi-asset coverage would need a
// JSONTable line-item list, a real but separate extension).
// recurring_sales_contract_id is an optional reference to Stage 37.6's
// RecurringSalesContract for the billing leg, rather than extending that
// doctype's own shape.
// ---------------------------------------------------------------------------

func ValidateServiceContractDocument(tenantID string, payload map[string]interface{}) error {
	startDate := pimString(payload["start_date"])
	endDate := pimString(payload["end_date"])
	if startDate != "" && endDate != "" && endDate < startDate {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "End Date", Message: "end_date cannot be before start_date"}
	}
	if visits, ok := parityNumber(payload["visits_included"]); ok && visits <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Visits Included", Message: "visits_included must be greater than zero"}
	}
	if recurringID := pimString(payload["recurring_sales_contract_id"]); recurringID != "" {
		if _, _, err := fetchDocData(tenantID, "RecurringSalesContract", recurringID); err != nil {
			return &ValidationError{Code: "META-0198", SubFor: "Billing Contract", Message: fmt.Sprintf("Linked RecurringSalesContract record with ID %q does not exist", recurringID)}
		}
	}
	return nil
}

// ConsumeServiceContractVisit increments visits_used, refusing once the
// contract's entitlement is exhausted - the caller (CloseServiceTicket)
// logs rather than fails the ticket close on this error, since the visit
// physically happened regardless of what the contract's own counter says.
func ConsumeServiceContractVisit(tenantID, contractID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchDocData(tenantID, "ServiceContract", contractID)
	if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("service contract %s is not Active (status: %s)", contractID, status)
	}
	visitsIncluded, _ := parityNumber(data["visits_included"])
	visitsUsed, _ := parityNumber(data["visits_used"])
	if visitsUsed >= visitsIncluded {
		return fmt.Errorf("service contract %s has no visits remaining (%v/%v used)", contractID, visitsUsed, visitsIncluded)
	}
	data["visits_used"] = visitsUsed + 1
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ServiceContract' AND id = $2`, schema),
		marshaled, contractID)
	return err
}

// ---------------------------------------------------------------------------
// 37.8.4: Service SLA breach reporting - GetSLABreaches's own SQL-side
// elapsed-time shape (engines/optimization.go), scoped to ServiceTicket's
// own respond_by_date/resolve_by_date fields. Date-only (not timestamp),
// this codebase's established SLA-threshold convention (Stage 37.4.4's
// dunning thresholds, SalesInvoice.due_date), so there is no
// naive-timestamp/timezone class of bug to inherit from GetSLABreaches's
// own documented history.
// ---------------------------------------------------------------------------

func GetServiceSLABreaches(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'customer', ''), status,
		       COALESCE(data->>'respond_by_date', ''), COALESCE(data->>'resolve_by_date', '')
		FROM %s.documents
		WHERE doctype = 'ServiceTicket' AND deleted_at IS NULL
		  AND status NOT IN ('Resolved', 'Closed', 'Cancelled')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	today := time.Now().Format("2006-01-02")
	var out []map[string]interface{}
	for rows.Next() {
		var id, customer, status, respondBy, resolveBy string
		if err := rows.Scan(&id, &customer, &status, &respondBy, &resolveBy); err != nil {
			return nil, err
		}
		var breachType string
		if status == "Draft" && respondBy != "" && respondBy < today {
			breachType = "Response"
		} else if resolveBy != "" && resolveBy < today {
			breachType = "Resolution"
		}
		if breachType == "" {
			continue
		}
		out = append(out, map[string]interface{}{
			"ticket_id": id, "customer": customer, "status": status, "breach_type": breachType,
			"respond_by_date": respondBy, "resolve_by_date": resolveBy,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "service-sla-breaches", Label: "Service SLA Breaches", Category: "Service",
		Columns: []ReportColumn{
			{Key: "ticket_id", Label: "Ticket"}, {Key: "customer", Label: "Customer"}, {Key: "status", Label: "Status"},
			{Key: "breach_type", Label: "Breach Type"}, {Key: "respond_by_date", Label: "Respond By"}, {Key: "resolve_by_date", Label: "Resolve By"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetServiceSLABreaches(tenantID)
		},
	})
}
