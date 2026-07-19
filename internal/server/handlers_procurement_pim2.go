package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"custom_erp/engines"
)

// RFQ/vendor quote comparison, sticker/barcode printing, payroll export, and
// the PIM V2 surface: dashboard, bulk edit, reports, Workbench, completeness
// scoring, media library, and the channel-publish queue.

func handleGetVendorQuotesForRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rfqID := r.URL.Query().Get("rfq_id")
	if rfqID == "" {
		http.Error(w, "Query parameter 'rfq_id' is required", http.StatusBadRequest)
		return
	}
	results, err := engines.GetVendorQuotesForRFQ(tenantID, rfqID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleSelectWinningQuote(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RfqID   string `json:"rfq_id"`
		QuoteID string `json:"quote_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RfqID == "" || req.QuoteID == "" {
		http.Error(w, "Fields 'rfq_id' and 'quote_id' are required", http.StatusBadRequest)
		return
	}
	if err := engines.SelectWinningQuote(tenantID, req.RfqID, req.QuoteID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "selected"})
}

// Sticker / Barcode Printing (Stage 13.15). Printer master creation/listing
// go through the existing generic doc endpoint like Vendor/Customer/RFQ did;
// these two handlers cover the print action and history, which need logic
// the generic endpoint doesn't have.
func handlePrintStickers(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Skus          []string `json:"skus"`
		PrinterCode   string   `json:"printer_code"`
		ReprintReason string   `json:"reprint_reason"`
		Copies        int      `json:"copies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	if req.PrinterCode == "" {
		http.Error(w, "Field 'printer_code' is required", http.StatusBadRequest)
		return
	}
	labels, err := engines.PrintStickers(tenantID, req.Skus, req.PrinterCode, userID, req.ReprintReason, req.Copies)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRINT_STICKERS", "SUCCESS", fmt.Sprintf("Printed %d sticker(s) on %s", len(labels), req.PrinterCode))
	_ = json.NewEncoder(w).Encode(labels)
}

func handlePrintHistory(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetPrintHistory(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.PrintHistoryEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handlePayrollExport implements MB 16.3's "Payroll Interface": exports
// approved attendance/leave data for an external payroll system to consume.
func handlePayrollExport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		http.Error(w, "Query parameters 'from' and 'to' are required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	results, err := engines.GetPayrollExport(tenantID, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.PayrollExportEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// PIM Foundation MVP (Stage 15). Family/Attribute/Content CRUD all use the
// same generic doc endpoint as Vendor/Customer/Employee; these two handlers
// cover the Product Workbench (blueprint section 7/18) and its per-item
// drill-down, which need the completeness-scoring logic the generic
// endpoint doesn't have.
func handlePIMDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	dashboard, err := engines.GetPIMDashboard(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(dashboard)
}

func handlePIMBulkEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Doctype string      `json:"doctype"`
		IDs     []string    `json:"ids"`
		Field   string      `json:"field"`
		Value   interface{} `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload JSON", http.StatusBadRequest)
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	allowed, err := checkPermission(tenantID, role, req.Doctype, "update")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("You do not have permission to update %s documents.", req.Doctype)})
		return
	}
	updatedIDs, err := engines.BulkUpdateDocuments(tenantID, req.Doctype, req.IDs, req.Field, req.Value, userID, role)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "updated", "updated_count": len(updatedIDs), "ids": updatedIDs,
	})
}

func handlePIMReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rows, err := engines.ListPIMReport(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("name"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func handlePIMWorkbench(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	family := r.URL.Query().Get("family")
	results, err := engines.ListWorkbench(tenantID, family)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.WorkbenchEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handlePIMCompleteness(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	itemCode := r.PathValue("itemCode")
	locale := r.URL.Query().Get("locale")
	channelID := r.URL.Query().Get("channel")
	result, err := engines.CalculateCompleteness(tenantID, itemCode, locale, channelID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handlePIMMediaUpload (Stage 15.2). Media files are larger than the global
// 2MB JSON body cap set in apiMiddleware - re-wrapping r.Body with a bigger
// MaxBytesReader here, before ParseMultipartForm reads it, raises the limit
// for this route only; every other route keeps the existing 2MB ceiling.
func handlePIMMediaUpload(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Upload exceeds 10MB limit or is malformed", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
		return
	}

	itemCode := r.FormValue("item")
	mediaRole := r.FormValue("media_role")

	asset, err := engines.SaveMediaFile(tenantID, fileBytes, header.Filename, itemCode, mediaRole, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(asset)
}

// handlePIMMediaFile streams a stored media file back - authenticated (this
// route sits behind apiMiddleware+moduleGate like every other PIM route),
// not the unauthenticated static file server public/ uses (routes.go's
// http.FileServer(http.Dir("./public"))) - the pragmatic in-house
// equivalent of "private storage + signed URL" (see engines/pim_media.go).
func handlePIMMediaFile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mediaID := r.PathValue("id")
	path, fileType, err := engines.GetMediaFile(tenantID, mediaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "stored file missing", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", fileType)
	_, _ = w.Write(fileBytes)
}

func handlePIMMediaList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	itemCode := r.URL.Query().Get("item")
	if itemCode == "" {
		http.Error(w, "item query parameter is required", http.StatusBadRequest)
		return
	}
	results, err := engines.ListMediaForItem(tenantID, itemCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.ProductMediaAsset{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handlePIMMediaDeactivate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mediaID := r.PathValue("id")
	if err := engines.DeactivateMedia(tenantID, mediaID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deactivated"})
}

// handlePIMPublish (Stage 15.2) queues a publish job after a readiness
// check - see engines.QueuePublish/CheckPublishReadiness for the rules and
// the stub-connector caveat (no real channel credentials exist in this
// environment).
func handlePIMPublish(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ItemCode string `json:"item_code"`
		Channel  string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	jobID, alreadyQueued, err := engines.QueuePublish(tenantID, req.ItemCode, req.Channel, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	// Bug fix (found during Stage 16.1 live verification): this used to
	// hardcode "status": "Queued" unconditionally, even when alreadyQueued
	// returns an existing job that's already Published (or Failed) -
	// misleading the caller into thinking a fresh, unprocessed job was
	// created. Look up the job's real current status instead.
	status := "Queued"
	if jobStatus, errStatus := engines.GetPublishJobStatus(tenantID, jobID); errStatus == nil {
		status = jobStatus.Status
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id": jobID, "already_queued": alreadyQueued, "status": status,
	})
}

func handlePIMPublishJobStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	jobID, err := strconv.Atoi(r.PathValue("jobID"))
	if err != nil {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}
	status, err := engines.GetPublishJobStatus(tenantID, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}

func handlePIMPublishLog(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	itemCode := r.URL.Query().Get("item")
	if itemCode == "" {
		http.Error(w, "item query parameter is required", http.StatusBadRequest)
		return
	}
	results, err := engines.ListPublishLogForItem(tenantID, itemCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.PublishLogEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// Fixed Asset Management (Stage 13.13b). Asset creation/listing use the
// same generic doc endpoint as Vendor/Customer/RFQ/Printer/Employee; these
// handlers cover the lifecycle actions (capitalise/transfer/dispose) and
// the depreciation-calculated register view, which need logic the generic
// endpoint doesn't have.
