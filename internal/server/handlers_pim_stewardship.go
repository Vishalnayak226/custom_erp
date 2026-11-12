package server

import (
	"custom_erp/engines"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Stage 47.13.4 / 146.5.9: the stewardship register (read side; changes are
// StewardshipChange documents through the approval engine) and the per-channel
// image order of an item.

func handlePIMStewardship(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := pimDataGate(w, r, "read", "AttributeStewardship", "ProductAttributeDef")
	if !ok {
		return
	}
	rows, err := engines.StewardshipOverview(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	lk, err := engines.StewardshipLookups(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"attributes": rows, "lookups": lk})
}

func handlePIMStewardshipDetail(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := pimDataGate(w, r, "read", "AttributeStewardship", "ProductAttributeValue")
	if !ok {
		return
	}
	d, err := engines.StewardshipDetail(tenantID, r.PathValue("attribute"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func handlePIMMediaOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tenantID, _, ok := pimDataGate(w, r, "read", "ProductMedia")
		if !ok {
			return
		}
		media, positioned, err := engines.ListMediaForItemChannel(tenantID, r.URL.Query().Get("item"), r.URL.Query().Get("channel"))
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		images := media[:0:0]
		for _, m := range media {
			if m.Status == "Active" && strings.HasPrefix(m.FileType, "image/") {
				images = append(images, m)
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"images": images, "ordered": positioned})
		return
	}
	tenantID, userID, ok := pimDataGate(w, r, "update", "ProductMedia")
	if !ok {
		return
	}
	var req struct {
		Item    string   `json:"item"`
		Channel string   `json:"channel"`
		IDs     []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
		return
	}
	n, err := engines.SetChannelImageOrder(tenantID, userID, req.Item, req.Channel, req.IDs)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ordered": n})
}
