package server

import (
	"custom_erp/engines"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Stage 146.5.2 / 146.5.5 / 146.5.6: marketplace templates loaded from the
// marketplace's own file, design-level attribute values, and feed runs. Authoring
// of templates and feeds themselves uses the generic document API.

// pimDataGate checks the caller may do `action` on every listed doctype.
func pimDataGate(w http.ResponseWriter, r *http.Request, action string, doctypes ...string) (tenantID, userID string, ok bool) {
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID = r.Header.Get("Resolved-User-ID")
	for _, dt := range doctypes {
		allowed, err := checkPermission(tenantID, role, dt, action)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return "", "", false
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return "", "", false
		}
	}
	return tenantID, userID, true
}

// handlePIMChannelTemplateLoad reads a marketplace category file (multipart
// `file`) and makes or refreshes the template for channel + category.
func handlePIMChannelTemplateLoad(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := pimDataGate(w, r, "create", "ChannelCategoryTemplate")
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Send the template file as multipart field 'file'.")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Send the template file as multipart field 'file'.")
		return
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 20<<20))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The file could not be read.")
		return
	}
	res, err := engines.LoadChannelTemplate(tenantID, userID, r.FormValue("channel"), r.FormValue("channel_category"), body, engines.ChannelTemplateOptions{
		ParentRows:      r.FormValue("parent_rows") == "Yes",
		ParentageColumn: r.FormValue("parentage_column"), ParentSKUColumn: r.FormValue("parent_sku_column"),
		ThemeColumn: r.FormValue("variation_theme_column"), SKUColumn: r.FormValue("sku_column"),
		FileName: hdr.Filename,
	})
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handlePIMChannelTemplateCoverage tells how many items fill each template column.
func handlePIMChannelTemplateCoverage(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := pimDataGate(w, r, "read", "ChannelCategoryTemplate", "Item")
	if !ok {
		return
	}
	cov, err := engines.ChannelTemplateCoverage(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, cov)
}

// handlePIMFeedRun runs a feed now. A download feed returns the file (run
// details in headers); an email feed returns the run as JSON.
func handlePIMFeedRun(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := pimDataGate(w, r, "read", "PIMFeed", "Item")
	if !ok {
		return
	}
	res, err := engines.RunPIMFeed(tenantID, userID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if res.Destination == "Email" {
		writeJSON(w, http.StatusOK, res)
		return
	}
	skipped := res.Skipped
	if len(skipped) > 50 {
		skipped = skipped[:50]
	}
	if b, mErr := json.Marshal(skipped); mErr == nil {
		w.Header().Set("X-Export-Skipped", url.QueryEscape(string(b)))
	}
	w.Header().Set("X-Export-Skipped-Count", strconv.Itoa(len(res.Skipped)))
	w.Header().Set("X-Export-Rows", strconv.Itoa(res.Rows))
	w.Header().Set("X-Feed-Run", res.RunID)
	w.Header().Set("Content-Type", res.ContentType)
	name := csvAttachmentName("pim_feed", r.PathValue("id"))
	if res.Extension != "csv" {
		name = strings.TrimSuffix(name, ".csv") + "." + res.Extension
	}
	w.Header().Set("Content-Disposition", attachmentDisposition(name))
	_, _ = w.Write(res.Data)
}

// handlePIMDesignValues: GET lists the designs of a product group with their own
// values (?group=&attributes=a,b); POST sets one attribute on many designs.
func handlePIMDesignValues(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tenantID, _, ok := pimDataGate(w, r, "read", "ProductAttributeValue", "Item")
		if !ok {
			return
		}
		var attrs []string
		for _, a := range strings.Split(r.URL.Query().Get("attributes"), ",") {
			if a = strings.TrimSpace(a); a != "" {
				attrs = append(attrs, a)
			}
		}
		rows, err := engines.DesignValueGrid(tenantID, r.URL.Query().Get("group"), attrs)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"designs": rows})
		return
	}
	tenantID, userID, ok := pimDataGate(w, r, "update", "ProductAttributeValue")
	if !ok {
		return
	}
	var req engines.DesignValueRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
		return
	}
	res, err := engines.SetDesignAttributeValues(tenantID, userID, req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Stage 146.5.5: add or remove tags on the content of a group's products or of
// designs and their variants.
func handlePIMDesignTags(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := pimDataGate(w, r, "update", "ProductContent")
	if !ok {
		return
	}
	var req engines.ItemTagRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
		return
	}
	res, err := engines.BulkTagItems(tenantID, userID, r.Header.Get("Resolved-Role"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// Stage 146.5.10: tasks with an attribute checklist. GET lists them with
// progress (reading completes the finished ones), POST makes one.
func handlePIMChecklists(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tenantID, userID, ok := pimDataGate(w, r, "read", "PIMTask")
		if !ok {
			return
		}
		rows, err := engines.ListPIMChecklistTasks(tenantID, userID)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"checklists": rows})
		return
	}
	tenantID, userID, ok := pimDataGate(w, r, "create", "PIMTask")
	if !ok {
		return
	}
	var req engines.PIMChecklistRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
		return
	}
	id, err := engines.CreatePIMChecklistTask(tenantID, userID, req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task_id": id})
}

// handlePIMChecklist reads one checklist: per product, what is left.
func handlePIMChecklist(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := pimDataGate(w, r, "read", "PIMTask")
	if !ok {
		return
	}
	cl, err := engines.PIMTaskChecklistFor(tenantID, userID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, cl)
}
