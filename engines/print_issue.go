package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 130.16 (74.1.4, BRD 4.8, DEC-17): the issued-output snapshot and the
// print / reprint log. The first print of an issued document (not a Draft,
// not waiting for approval) freezes what it printed - party, store, GSTIN,
// prices, tax - and every later print renders that frozen copy marked
// REPRINT, so a reprint is the original again, not today's masters. A
// document whose amendment reason changes prints as its next revision. Who
// printed and when come from the server.
//
// Reprint control (print.reprint_control, off by default - old clients are
// unchanged) adds the DocumentReprint permission, a reason, and - after
// print.reprint_free_count reprints, or for any doctype listed in
// print.reprint_approval_doctypes - an approved Reprint Request.

const (
	SettingReprintControl          = "print.reprint_control"
	SettingReprintFreeCount        = "print.reprint_free_count"
	SettingReprintApprovalDoctypes = "print.reprint_approval_doctypes"
	ReprintRequestDoctype          = "ReprintRequest"
	DocumentReprintDoctype         = "DocumentReprint" // the permission to reprint
)

// Print outcomes: how far a print is known to have got. A browser print is
// only ever handed to the browser and a PrintBridge job submitted; nothing
// here confirms paper, so nothing is logged "Printed".
const (
	PrintOutcomeSentToBrowser = "Sent to browser"
	PrintOutcomeSubmitted     = "Submitted"
	PrintOutcomeUnknown       = "Unknown"
)

// printNotIssued are statuses whose print issues nothing: a draft or a
// request is not the document yet, and a cancelled one prints as it is now.
var printNotIssued = map[string]bool{"": true, "Draft": true, "Pending Approval": true, "Rejected": true, "Cancelled": true}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingReprintControl, Module: "Printing",
		Label: "Control reprints", Type: SettingTypeBool, Default: "false",
		Description: "On: printing a document again needs the Document Reprint permission (Roles) and a reason, and past the free reprints below - or for the record types listed below - an approved Reprint Request (Settings > Approval Rules: add a rule for ReprintRequest first). Off: a reprint is still logged and marked REPRINT.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingReprintFreeCount, Module: "Printing",
		Label: "Reprints before approval is needed", Type: SettingTypeInt, Default: "1", Unit: "reprints",
		Description: "With reprint control on, how many times a document can be printed again before a reprint needs approval. 0: every reprint needs approval.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingReprintApprovalDoctypes, Module: "Printing",
		Label: "Record types whose every reprint needs approval", Type: SettingTypeString, Default: "",
		Description: "With reprint control on, comma-separated record types (for example SalesInvoice, CreditNote, DeliveryChallan) that need an approved Reprint Request for any reprint.",
	})
	RegisterApprovalDecisionHook(ReprintRequestDoctype, true, func(tenantID, docID, decision, actorUserID string) {})
}

// PrintIssue is what printing a document now means, sent with its print data.
type PrintIssue struct {
	Issued          bool   `json:"issued"`
	Revision        int    `json:"revision"`
	Reprint         bool   `json:"reprint"`
	PrintedBefore   int    `json:"printed_before"`
	FirstPrintedAt  string `json:"first_printed_at,omitempty"`
	FirstPrintedBy  string `json:"first_printed_by,omitempty"`
	PrintedBy       string `json:"printed_by"`
	PrintedAt       string `json:"printed_at"`
	Checksum        string `json:"checksum,omitempty"`
	Control         bool   `json:"control"`
	NeedsApproval   bool   `json:"needs_approval,omitempty"`
	ApprovedRequest string `json:"approved_request,omitempty"`
	PendingRequest  string `json:"pending_request,omitempty"`
}

type printSnapshot struct {
	Revision int
	Key      string
	Payload  []byte
	Checksum string
	FrozenBy string
	FrozenAt time.Time
}

func latestPrintSnapshot(q queryRower, schema, txn, id string) (*printSnapshot, error) {
	var s printSnapshot
	err := q.QueryRow(fmt.Sprintf(`SELECT revision, amendment_key, payload, checksum, COALESCE(frozen_by, ''), frozen_at
		FROM %s.print_snapshots WHERE txn = $1 AND doc_id = $2 ORDER BY revision DESC LIMIT 1`, schema), txn, id).
		Scan(&s.Revision, &s.Key, &s.Payload, &s.Checksum, &s.FrozenBy, &s.FrozenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// printAmendmentKey is what makes an amended document a new revision.
func printAmendmentKey(tenantID, doctype, id string) string {
	data, _, err := fetchDocData(tenantID, doctype, id)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strField(data, "amendment_reason"))
}

// printChecksum is the hash of what a print says - pictures aside.
func printChecksum(d PrintDocument) string {
	d.Issue = nil
	lines := make([]map[string]interface{}, len(d.Lines))
	for i, l := range d.Lines {
		c := map[string]interface{}{}
		for k, v := range l {
			if k != "image" {
				c[k] = v
			}
		}
		lines[i] = c
	}
	d.Lines = lines
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func printStamp(schema string, t time.Time) string {
	return t.In(crmLocation(schema)).Format("2006-01-02 15:04")
}

// IssuedPrint is the document to print now: once issued, its frozen copy
// (with today's pictures when they were asked for), else what was built.
func IssuedPrint(tenantID string, fresh PrintDocument, userName string) (PrintDocument, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return fresh, err
	}
	issue := &PrintIssue{Issued: !printNotIssued[fresh.Status], PrintedBy: userName, PrintedAt: printStamp(schema, time.Now()),
		Control: GetSettingBool(tenantID, SettingReprintControl)}
	snap, err := latestPrintSnapshot(db.DB, schema, fresh.Transaction, fresh.DocumentID)
	if err != nil {
		return fresh, err
	}
	out := fresh
	key := printAmendmentKey(tenantID, fresh.Doctype, fresh.DocumentID)
	var frozen PrintDocument
	if issue.Issued && snap != nil && snap.Key == key && json.Unmarshal(snap.Payload, &frozen) == nil {
		images := map[string]interface{}{}
		for _, l := range fresh.Lines {
			if img, ok := l["image"]; ok {
				images[strField(l, "sku")] = img
			}
		}
		for _, l := range frozen.Lines {
			if img, ok := images[strField(l, "sku")]; ok {
				l["image"] = img
			}
		}
		out = frozen
		issue.Reprint, issue.Revision, issue.Checksum = true, snap.Revision, snap.Checksum
		issue.FirstPrintedAt, issue.FirstPrintedBy = printStamp(schema, snap.FrozenAt), snap.FrozenBy
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.print_job_log WHERE txn = $1 AND doc_id = $2 AND doc_revision = $3 AND print_action IN ('Print', 'Reprint')`, schema),
			fresh.Transaction, fresh.DocumentID, snap.Revision).Scan(&issue.PrintedBefore)
		if issue.Control && reprintNeedsApproval(tenantID, fresh.Doctype, issue.PrintedBefore) {
			issue.NeedsApproval = true
			issue.ApprovedRequest, issue.PendingRequest = reprintRequestsFor(schema, fresh.Transaction, fresh.DocumentID, userName)
		}
	} else if issue.Issued {
		issue.Revision = 1
		if snap != nil {
			issue.Revision = snap.Revision + 1
		}
		issue.Checksum = printChecksum(fresh)
	}
	out.Issue = issue
	return out, nil
}

// reprintNeedsApproval: past the free reprints, or a listed record type.
// printedBefore counts the original too.
func reprintNeedsApproval(tenantID, doctype string, printedBefore int) bool {
	for _, d := range strings.Split(GetSettingString(tenantID, SettingReprintApprovalDoctypes), ",") {
		if strings.EqualFold(strings.TrimSpace(d), doctype) && doctype != "" {
			return true
		}
	}
	return printedBefore-1 >= GetSettingInt(tenantID, SettingReprintFreeCount)
}

// reprintRequestsFor finds this person's approved, unused request for the
// document, and one still waiting.
func reprintRequestsFor(schema, txn, docID, userName string) (approved, pending string) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status FROM %s.documents WHERE doctype = $1 AND data->>'txn' = $2 AND data->>'doc_id' = $3
		AND data->>'requested_by' = $4 AND status IN ('Approved', 'Pending Approval') ORDER BY created_at DESC`, schema), ReprintRequestDoctype, txn, docID, userName)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if rows.Scan(&id, &status) != nil {
			continue
		}
		if status == "Approved" && approved == "" {
			approved = id
		}
		if status == "Pending Approval" && pending == "" {
			pending = id
		}
	}
	return approved, pending
}

// RequestReprint sends a reprint to its approver (an approval rule on
// ReprintRequest says who).
func RequestReprint(tenantID, txn, docID, reason, userName, userID, role string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", &ValidationError{Code: "GLOBAL-1315", SubFor: "reason", Message: "say why this is being printed again"}
	}
	fresh, err := BuildPrintDocument(tenantID, txn, docID, "", false)
	if err != nil {
		return "", err
	}
	issued, err := IssuedPrint(tenantID, fresh, userName)
	if err != nil {
		return "", err
	}
	if !issued.Issue.Reprint {
		return "", &ValidationError{Code: "GLOBAL-1315", SubFor: "document", Message: "this document has not been printed yet - print it; no approval is needed"}
	}
	if issued.Issue.PendingRequest != "" {
		return issued.Issue.PendingRequest, nil
	}
	if gated, _ := IsApprovalGated(tenantID, ReprintRequestDoctype); !gated {
		return "", &ValidationError{Code: "GLOBAL-1315", SubFor: "approval", Message: "no one is set to approve reprints - an administrator adds an approval rule for ReprintRequest (Settings > Approval Rules)"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	id := NewDocID("RPR")
	raw, _ := json.Marshal(map[string]interface{}{"code": id, "number": firstNonEmpty(issued.Number, docID), "txn": txn, "doc_id": docID, "doctype": issued.Doctype,
		"reason": reason, "amount": issued.Issue.PrintedBefore, "requested_by": userName, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, ReprintRequestDoctype, raw, userID); err != nil {
		return "", err
	}
	if err := SubmitForApproval(tenantID, ReprintRequestDoctype, id, userID, role); err != nil {
		return id, err
	}
	LogAuditEvent(tenantID, userID, "REQUEST_REPRINT", "SUCCESS", fmt.Sprintf("%s: reprint %s %s - %s", id, txn, firstNonEmpty(issued.Number, docID), reason))
	return id, nil
}

// recordIssuedPrint is RecordDocumentPrint's server half: it works out
// original or reprint, enforces reprint control, freezes an original, and
// fills the log row's revision, checksum and outcome.
func recordIssuedPrint(tenantID, printedBy string, p *DocumentPrint) error {
	switch p.Channel {
	case "qz":
		p.Outcome = PrintOutcomeSubmitted
	case "browser":
		p.Outcome = PrintOutcomeSentToBrowser
	default:
		p.Outcome = PrintOutcomeUnknown
	}
	p.Reprinted = p.Action == PrintActionReprint
	p.TemplateID = templateVersionLabel(tenantID, p.TemplateID) // Stage 130.16 (74.3.4): which version printed
	// A document that cannot be built now is logged as it was printed -
	// nothing to freeze or compare it with; a log never fails a print.
	fresh, err := BuildPrintDocument(tenantID, p.Txn, p.DocID, p.Store, false)
	if err != nil {
		return nil
	}
	issued, err := IssuedPrint(tenantID, fresh, printedBy)
	if err != nil {
		return err
	}
	is := issued.Issue
	p.Revision, p.Checksum = is.Revision, is.Checksum
	p.Reprinted = p.Reprinted || is.Reprint
	paper := p.Action == PrintActionPrint || p.Action == PrintActionReprint
	if is.Reprint && is.Control && paper {
		if !p.MayReprint {
			return &ValidationError{Code: "GLOBAL-1315", SubFor: "permission", Message: "you may not print this document again - a Document Reprint permission is needed (Roles)"}
		}
		if strings.TrimSpace(p.ReprintReason) == "" {
			return &ValidationError{Code: "GLOBAL-1315", SubFor: "reason", Message: "say why this is being printed again"}
		}
		if is.NeedsApproval {
			if p.ApprovalID == "" || p.ApprovalID != is.ApprovedRequest {
				return &ValidationError{Code: "GLOBAL-1315", SubFor: "approval", Message: "this reprint needs approval - send a Reprint Request and print once it is approved"}
			}
			if p.CheckOnly {
				return nil
			}
			if err := useReprintRequest(tenantID, p); err != nil {
				return err
			}
		}
	}
	if is.Issued && !is.Reprint && !p.CheckOnly {
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(fresh)
		// frozen_at in UTC: the column has no zone and the session clock is local.
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.print_snapshots (txn, doc_id, revision, amendment_key, doctype, payload, checksum, frozen_by, frozen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (txn, doc_id, revision) DO NOTHING`, schema),
			p.Txn, p.DocID, is.Revision, printAmendmentKey(tenantID, fresh.Doctype, p.DocID), fresh.Doctype, string(raw), is.Checksum, printedBy, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

// useReprintRequest spends an approved request: one approval, one reprint.
func useReprintRequest(tenantID string, p *DocumentPrint) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var approver string
	err = db.DB.QueryRow(fmt.Sprintf(`UPDATE %s.documents SET status = 'Used', data = data || jsonb_build_object('used_at', $2::text), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $1 AND id = $3 AND status = 'Approved'
		RETURNING COALESCE((SELECT actor_user_id FROM %s.approval_log WHERE doctype = $1 AND document_id = $3 AND action = 'Approved' ORDER BY id DESC LIMIT 1), '')`, schema, schema),
		ReprintRequestDoctype, time.Now().UTC().Format(time.RFC3339), p.ApprovalID).Scan(&approver)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "GLOBAL-1315", SubFor: "approval", Message: "that approval has already been used - send a new Reprint Request"}
	}
	if err != nil {
		return err
	}
	p.ApprovedBy = approver
	return nil
}
