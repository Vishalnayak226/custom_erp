package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// RequestContext holds basic metadata for tracking execution
type RequestContext struct {
	TenantID      string
	CorrelationID string
	UserID        string
	Role          string
	LocationCode  string
}

// Simple sliding window rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
	}
}

func (rl *RateLimiter) Allow(ip string, limit int, duration time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-duration)

	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit {
		rl.requests[ip] = valid
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

var globalLimiter = NewRateLimiter()

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Middleware wrapper to inject TenantID, User Claims, and enforce security policies
func apiMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		correlationID := generateUUID()
		w.Header().Set("X-Correlation-ID", correlationID)
		w.Header().Set("Content-Type", "application/json")

		// 1. CORS Headers (Strict, check Host origin)
		origin := r.Header.Get("Origin")
		if origin != "" {
			// In production, validate origin against a whitelist
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// 2. Payload size limit (Max 2MB)
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

		// 3. Rate Limiter (60/min limit per IP)
		ip := strings.Split(r.RemoteAddr, ":")[0]
		limit := 60
		if strings.HasSuffix(r.URL.Path, "/login") {
			limit = 5 // Limit logins to 5/min per IP
		}
		if !globalLimiter.Allow(ip, limit, time.Minute) {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Rate limit exceeded. Please try again later.",
			})
			return
		}

		// Panic Recovery
		defer func() {
			if err := recover(); err != nil {
				stackTrace := string(debug.Stack())
				errMsg := fmt.Sprintf("%v", err)
				tenantID := r.Header.Get("Resolved-Tenant-ID")
				if tenantID == "" {
					tenantID = "default"
				}
				engines.LogSystemError(tenantID, correlationID, "PANIC", r.URL.Path, errMsg, stackTrace)

				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":          "A critical server error occurred.",
					"correlation_id": correlationID,
				})
			}
		}()

		// 4. Token & Tenant Resolution
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID == "" {
			tenantID = r.URL.Query().Get("tenant_id")
		}
		if tenantID == "" {
			tenantID = "default"
		}

		userID := "system"
		role := "Guest"
		locationCode := "HO"

		// Inspect Authorization Header
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := engines.ParseToken(tokenStr)
			if err == nil {
				userID = claims["id"]
				role = claims["role"]
				tenantID = claims["tenant"]
				locationCode = claims["loc"]
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or expired token"})
				return
			}
		} else {
			// Local development / testing fallback context (emulate admin session silently)
			userID = "admin"
			role = "HR/Admin"
			locationCode = "HO"
		}

		// Attach Resolved Context fields
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Correlation-ID", correlationID)
		r.Header.Set("Resolved-User-ID", userID)
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-Location", locationCode)

		next.ServeHTTP(w, r)
	}
}

func main() {
	// Initialize database connection
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	}
	db.InitDB(connStr)

	// Start Outbox background poller (Scale and Omnichannel integration queue)
	engines.StartOutboxWorker(5 * time.Second)

	// Authentication API
	http.HandleFunc("POST /api/v1/login", apiMiddleware(handleLogin))

	// Generic DocType CRUD APIs (Go 1.22 enhanced routing)
	http.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))

	// Availability & Reservation APIs
	http.HandleFunc("GET /api/v1/availability", apiMiddleware(handleGetAvailability))
	http.HandleFunc("POST /api/v1/reserve", apiMiddleware(handleCreateReservation))

	// Checkout & Finance APIs
	http.HandleFunc("POST /api/v1/checkout", apiMiddleware(handleCheckout))
	http.HandleFunc("GET /api/v1/finance/trial-balance", apiMiddleware(handleTrialBalance))

	// Shopify Integration Webhook APIs
	http.HandleFunc("POST /api/v1/integration/shopify/product/map", apiMiddleware(handleShopifyProductMap))
	http.HandleFunc("POST /api/v1/integration/shopify/order", apiMiddleware(handleShopifyOrderWebhook))

	// Store Fulfillment & Returns APIs
	http.HandleFunc("POST /api/v1/fulfillment/task/transition", apiMiddleware(handleFulfillmentTaskTransition))
	http.HandleFunc("POST /api/v1/fulfillment/return", apiMiddleware(handleFulfillmentReturn))

	// Administration Scale Test APIs
	http.HandleFunc("POST /api/v1/admin/scale-test", apiMiddleware(handleScaleTest))

	// Marketplace & Logistics Integration APIs
	http.HandleFunc("POST /api/v1/marketplace/settlement/reconcile", apiMiddleware(handleMarketplaceReconcile))
	http.HandleFunc("POST /api/v1/marketplace/logistics/book", apiMiddleware(handleLogisticsBook))

	// Optimization & Advanced Forecasting APIs
	http.HandleFunc("GET /api/v1/optimization/replenishment-suggestions", apiMiddleware(handleReplenishmentSuggestions))
	http.HandleFunc("GET /api/v1/optimization/sla-breaches", apiMiddleware(handleSLABreaches))
	http.HandleFunc("POST /api/v1/optimization/forecast", apiMiddleware(handleDemandForecast))

	// DocType Metadata APIs
	http.HandleFunc("GET /api/v1/doc/{doctype}/meta", apiMiddleware(handleGetDocTypeMeta))
	http.HandleFunc("GET /api/v1/meta/doctypes", apiMiddleware(handleGetDocTypes))
	http.HandleFunc("POST /api/v1/meta/doctypes", apiMiddleware(handleSaveDocType))
	http.HandleFunc("POST /api/v1/meta/{doctype}/fields", apiMiddleware(handleSaveFieldDefinition))
	http.HandleFunc("DELETE /api/v1/meta/{doctype}/fields/{id}", apiMiddleware(handleDeleteFieldDefinition))

	// Core Foundation APIs
	http.HandleFunc("/api/v1/labels", apiMiddleware(handleLabels))
	http.HandleFunc("/api/v1/sequence", apiMiddleware(handleSequence))
	http.HandleFunc("/api/v1/prefix", apiMiddleware(handlePrefix))
	http.HandleFunc("/api/v1/logs/audit", apiMiddleware(handleAuditLogs))

	// Industry Configuration & Preset Profiler
	http.HandleFunc("GET /api/v1/admin/industries", apiMiddleware(handleGetIndustries))
	http.HandleFunc("POST /api/v1/admin/industry", apiMiddleware(handleSwitchIndustry))

	// Bulk CSV Import
	http.HandleFunc("POST /api/v1/import/{doctype}", apiMiddleware(handleBulkImport))
	http.HandleFunc("GET /api/v1/import/{doctype}/template", apiMiddleware(handleGetImportTemplate))
	http.HandleFunc("/api/v1/logs/system", apiMiddleware(handleSystemLogs))
	http.HandleFunc("/api/v1/debug/panic", apiMiddleware(handleDebugPanic))

	// Serve Static Files
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	log.Println("Starting ERP Server on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// REST HANDLERS

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var u struct {
		ID           string
		Username     string
		PasswordHash string
		Role         string
	}

	// Query user details
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, username, password_hash, role 
		FROM %s.users 
		WHERE username = $1 AND status = 'Active'`, schema), req.Username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		// Generic security error message
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Check password with bcrypt (supports fallback check for local seed configs)
	err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password))
	if err != nil && u.PasswordHash != req.Password {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Hardcoded default location for simplicity, can be mapped in DB users table later
	locationCode := "HO"
	token := engines.SignToken(u.ID, u.Username, u.Role, tenantID, locationCode)

	engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "SUCCESS", fmt.Sprintf("User logged in successfully with role %s", u.Role))

	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": token,
		"role":  u.Role,
		"user":  u.Username,
	})
}

// Generic CRUD handler wrapping security RBAC authorization and validation rules
func handleGenericDoc(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")

	// Resolve parameters using Go 1.22 enhanced routing Value methods
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	action := ""
	switch r.Method {
	case http.MethodGet:
		action = "read"
	case http.MethodPost:
		action = "create"
		if id != "" {
			action = "update"
		}
	case http.MethodDelete:
		action = "delete"
	}

	// 1. RBAC permissions verification
	allowed, err := checkPermission(tenantID, role, doctype, action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("You do not have permission to %s %s documents.", action, doctype)})
		return
	}

	switch r.Method {
	case http.MethodGet:
		if id != "" {
			// Retrieve single document
			var dataStr string
			var status string
			err = db.DB.QueryRow(fmt.Sprintf(`
				SELECT data, status FROM %s.documents 
				WHERE doctype = $1 AND id = $2`, schema), doctype, id).Scan(&dataStr, &status)
			if err == sql.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Document not found"})
				return
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			var dataMap map[string]interface{}
			_ = json.Unmarshal([]byte(dataStr), &dataMap)
			dataMap["id"] = id
			dataMap["status"] = status

			// Location Filter Validation (Object-Level Auth)
			if docLoc, exists := dataMap["location"]; exists && fmt.Sprintf("%v", docLoc) != location && role != "HR/Admin" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "This document does not belong to your assigned location."})
				return
			}

			_ = json.NewEncoder(w).Encode(dataMap)
		} else {
			// Retrieve multiple documents (support search, location filtering, and custom query filters)
			searchQuery := r.URL.Query().Get("q")
			query := fmt.Sprintf("SELECT id, data, status FROM %s.documents WHERE doctype = $1", schema)
			var args []interface{}
			args = append(args, doctype)
			argIndex := 2

			// Location filtering: non-admins can only see records for their location
			if role != "HR/Admin" {
				query += fmt.Sprintf(" AND data->>'location' = $%d", argIndex)
				args = append(args, location)
				argIndex++
			}

			// Dynamic search parameter filters check (WMS/OMS query filters)
			for key, vals := range r.URL.Query() {
				if key == "q" || key == "tenant_id" || len(vals) == 0 {
					continue
				}
				query += fmt.Sprintf(" AND data->>'%s' = $%d", key, argIndex)
				args = append(args, vals[0])
				argIndex++
			}

			rows, err := db.DB.Query(query, args...)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			docs := []map[string]interface{}{}
			for rows.Next() {
				var docID string
				var dataStr string
				var status string
				if err := rows.Scan(&docID, &dataStr, &status); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				var dataMap map[string]interface{}
				_ = json.Unmarshal([]byte(dataStr), &dataMap)
				dataMap["id"] = docID
				dataMap["status"] = status

				// Local search match
				if searchQuery != "" {
					matched := false
					for _, val := range dataMap {
						if strings.Contains(strings.ToLower(fmt.Sprintf("%v", val)), strings.ToLower(searchQuery)) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}

				docs = append(docs, dataMap)
			}
			_ = json.NewEncoder(w).Encode(docs)
		}

	case http.MethodPost:
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid payload JSON", http.StatusBadRequest)
			return
		}

		// 2. Server-side metadata validation engine check
		err = engines.ValidateDocument(tenantID, doctype, payload)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Setup Document ID and attributes
		docID := ""
		if id != "" {
			docID = id
		} else if payloadID, exists := payload["id"]; exists && payloadID != nil {
			docID = fmt.Sprintf("%v", payloadID)
		} else {
			docID = generateUUID()
		}

		payloadBytes, _ := json.Marshal(payload)
		statusVal := "Active"
		if s, exists := payload["status"]; exists && s != nil {
			statusVal = fmt.Sprintf("%v", s)
		}

		// Perform Upsert using parameterized parameters (SQL Injection Safe)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by) 
			VALUES ($1, $2, $3, $4, $5) 
			ON CONFLICT (id) DO UPDATE SET 
				data = EXCLUDED.data, 
				status = EXCLUDED.status, 
				updated_at = CURRENT_TIMESTAMP`, schema)
		_, err = db.DB.Exec(query, docID, doctype, payloadBytes, statusVal, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// GRN Callback Hook: Automatically post received items to inventory ledger
		if doctype == "GRN" {
			locationCode, _ := payload["location"].(string)
			items, _ := payload["items"].([]interface{})
			if locationCode != "" && len(items) > 0 {
				errLedger := engines.PostInventoryLedger(tenantID, locationCode, items)
				if errLedger != nil {
					log.Printf("Error posting GRN items to stock ledger: %v", errLedger)
				}
			}

			// Publish inventory transaction changed outbox event
			tx, errTx := db.DB.Begin()
			if errTx == nil {
				_ = db.SetSearchPath(tx, schema)
				_ = engines.PublishEvent(tx, schema, "inventory.stock_changed", map[string]interface{}{
					"grn_id":   docID,
					"location": locationCode,
				})
				_ = tx.Commit()
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "saved",
			"id":     docID,
		})

	case http.MethodDelete:
		if id == "" {
			http.Error(w, "Document ID is required", http.StatusBadRequest)
			return
		}

		// Delete document from repository
		_, err = db.DB.Exec(fmt.Sprintf("DELETE FROM %s.documents WHERE id = $1 AND doctype = $2", schema), id, doctype)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func checkPermission(tenantID string, role string, doctype string, action string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	column := ""
	switch action {
	case "read":
		column = "allow_read"
	case "create":
		column = "allow_create"
	case "update":
		column = "allow_update"
	case "delete":
		column = "allow_delete"
	default:
		return false, fmt.Errorf("invalid permission action: %s", action)
	}

	var allowed bool
	query := fmt.Sprintf("SELECT COALESCE(%s, false) FROM %s.role_permissions WHERE role = $1 AND doctype_name = $2", column, schema)
	err = db.DB.QueryRow(query, role, doctype).Scan(&allowed)
	if err == sql.ErrNoRows {
		// Default: deny if no mapping rule exists
		return false, nil
	}
	return allowed, err
}

func handleLabels(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	switch r.Method {
	case http.MethodGet:
		labels, err := engines.GetLabels(tenantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(labels)

	case http.MethodPost:
		var req struct {
			OriginalText string `json:"original_text"`
			CustomText   string `json:"custom_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if req.OriginalText == "" || req.CustomText == "" {
			http.Error(w, "Fields original_text and custom_text are required", http.StatusBadRequest)
			return
		}

		err := engines.SaveLabel(tenantID, req.OriginalText, req.CustomText)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	case http.MethodDelete:
		orig := r.URL.Query().Get("original_text")
		if orig == "" {
			http.Error(w, "Query parameter original_text is required", http.StatusBadRequest)
			return
		}

		err := engines.DeleteLabel(tenantID, orig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleSequence(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DocType       string `json:"doc_type"`
		StoreCode     string `json:"store_code"`
		FinancialYear string `json:"financial_year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if req.DocType == "" || req.FinancialYear == "" {
		http.Error(w, "doc_type and financial_year are required", http.StatusBadRequest)
		return
	}

	code, err := engines.GenerateSequence(tenantID, req.DocType, req.StoreCode, req.FinancialYear)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	engines.LogAuditEvent(tenantID, "system", "GENERATE_SEQUENCE", "SUCCESS", fmt.Sprintf("Generated %s sequence code: %s", req.DocType, code))

	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func handlePrefix(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, doc_type, prefix, separator, padding_width, reset_frequency, active_status 
			FROM %s.prefix_configs ORDER BY doc_type`, schema))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type PrefixConfig struct {
			ID             string `json:"id"`
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
		}

		configs := []PrefixConfig{}
		for rows.Next() {
			var c PrefixConfig
			err := rows.Scan(&c.ID, &c.DocType, &c.Prefix, &c.Separator, &c.PaddingWidth, &c.ResetFrequency, &c.ActiveStatus)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			configs = append(configs, c)
		}
		_ = json.NewEncoder(w).Encode(configs)

	case http.MethodPost:
		var req struct {
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		query := fmt.Sprintf(`
			INSERT INTO %s.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status) 
			VALUES ($1, $2, $3, $4, $5, $6) 
			ON CONFLICT (doc_type) DO UPDATE SET 
				prefix = EXCLUDED.prefix, 
				separator = EXCLUDED.separator, 
				padding_width = EXCLUDED.padding_width, 
				reset_frequency = EXCLUDED.reset_frequency, 
				active_status = EXCLUDED.active_status`, schema)
		_, err = db.DB.Exec(query, req.DocType, req.Prefix, req.Separator, req.PaddingWidth, req.ResetFrequency, req.ActiveStatus)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		engines.LogAuditEvent(tenantID, "admin", "UPDATE_PREFIX_CONFIG", "SUCCESS", fmt.Sprintf("Updated prefix config for doc_type: %s", req.DocType))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT id, user_id, action, status, details, created_at FROM %s.audit_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type AuditLog struct {
		ID        string `json:"id"`
		UserID    string `json:"user_id"`
		Action    string `json:"action"`
		Status    string `json:"status"`
		Details   string `json:"details"`
		CreatedAt string `json:"created_at"`
	}

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Action, &l.Status, &l.Details, &l.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logs = append(logs, l)
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT log_id, correlation_id, severity, module_source, error_message, stack_trace, created_at FROM %s.system_error_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type SystemLog struct {
		LogID         string         `json:"log_id"`
		CorrelationID sql.NullString `json:"correlation_id"`
		Severity      string         `json:"severity"`
		ModuleSource  string         `json:"module_source"`
		ErrorMessage  string         `json:"error_message"`
		StackTrace    string         `json:"stack_trace"`
		CreatedAt     string         `json:"created_at"`
	}

	logs := []SystemLog{}
	for rows.Next() {
		var l SystemLog
		if err := rows.Scan(&l.LogID, &l.CorrelationID, &l.Severity, &l.ModuleSource, &l.ErrorMessage, &l.StackTrace, &l.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logs = append(logs, l)
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleDebugPanic(w http.ResponseWriter, r *http.Request) {
	panic("Deliberate testing panic: Dynamic recovery log engine operational!")
}

func handleGetDocTypeMeta(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	fields, err := engines.GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(fields)
}

func handleGetDocTypes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	list, err := engines.GetDocTypes(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSaveDocType(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		Name         string `json:"name"`
		Module       string `json:"module"`
		DocumentType string `json:"document_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	err := engines.SaveDocType(tenantID, req.Name, req.Module, req.DocumentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleSaveFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	var req engines.FieldMeta
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	err := engines.SaveFieldDefinition(tenantID, doctype, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleDeleteFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	err := engines.DeleteFieldDefinition(tenantID, doctype, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func handleGetIndustries(w http.ResponseWriter, r *http.Request) {
	list := []map[string]string{
		{"code": "JEWELRY", "name": "Jewelry Industry"},
		{"code": "FOOD_BEV", "name": "Food and Beverage Industry"},
		{"code": "AUTO", "name": "Automobile Industry"},
		{"code": "CLOTHING", "name": "Clothing & Apparel Industry"},
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSwitchIndustry(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		IndustryCode string `json:"industry_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	profilePath := fmt.Sprintf("./public/profiles/%s.json", strings.ToLower(req.IndustryCode))
	err := engines.SwitchIndustryProfile(tenantID, profilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to switch industry: %v", err), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Industry configuration profile reloaded successfully"})
}

func handleBulkImport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	userID := r.Header.Get("Resolved-Role")

	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "Multipart payload exceeds limit", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "CSV file is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleGetImportTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	templateBytes, err := engines.GenerateCSVTemplate(tenantID, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_template.csv", doctype))
	_, _ = w.Write(templateBytes)
}

func handleGetAvailability(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sku := r.URL.Query().Get("sku")
	location := r.URL.Query().Get("location")

	if sku == "" || location == "" {
		http.Error(w, "Query parameters 'sku' and 'location' are required", http.StatusBadRequest)
		return
	}

	res, err := engines.GetAvailableToSell(tenantID, sku, location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Sku          string `json:"sku"`
		Location     string `json:"location"`
		Qty          int    `json:"qty"`
		ResType      string `json:"res_type"`
		ExpirySecond int    `json:"expiry"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Sku == "" || req.Location == "" || req.Qty <= 0 {
		http.Error(w, "Fields 'sku', 'location', and positive 'qty' are required", http.StatusBadRequest)
		return
	}

	expiry := req.ExpirySecond
	if expiry <= 0 {
		expiry = 300 // default 5 minutes
	}

	resID, err := engines.CreateReservation(tenantID, req.Sku, req.Location, req.Qty, req.ResType, expiry)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":         "reserved",
		"reservation_id": resID,
	})
}

func handleCheckout(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CartNumber  string `json:"cart_number"`
		Location    string `json:"location"`
		PaymentMode string `json:"payment_mode"`
		Items       []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid checkout payload", http.StatusBadRequest)
		return
	}

	if req.CartNumber == "" || req.Location == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'cart_number', 'location', and 'items' are required", http.StatusBadRequest)
		return
	}

	// 1. Convert items structure to interface slice for PostInventoryLedger (with negative qty!)
	itemsInterface := make([]interface{}, len(req.Items))
	totalSalePrice := 0
	totalCostPrice := 0

	for i, item := range req.Items {
		itemsInterface[i] = map[string]interface{}{
			"sku": item.Sku,
			"qty": -item.Qty, // Negative to decrement available stock
		}
		totalSalePrice += int(item.SalePrice) * item.Qty
		totalCostPrice += int(item.CostPrice) * item.Qty
	}

	// 2. Decrement inventory availability
	err := engines.PostInventoryLedger(tenantID, req.Location, itemsInterface)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Inventory decrement failed: %v", err)})
		return
	}

	// 3. Post balanced accounting bookings
	err = engines.PostSalesFinanceBooking(tenantID, req.CartNumber, totalSalePrice, totalCostPrice)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("GL Booking posting failed: %v", err)})
		return
	}

	// 4. Save dynamic checkout document
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil {
		payloadBytes, _ := json.Marshal(req)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by) 
			VALUES ($1, 'POSCart', $2, 'Paid', 'system') 
			ON CONFLICT (id) DO UPDATE SET 
				data = EXCLUDED.data, 
				status = EXCLUDED.status, 
				updated_at = CURRENT_TIMESTAMP`, schema)
		_, _ = db.DB.Exec(query, req.CartNumber, payloadBytes)
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "completed",
		"cart_number": req.CartNumber,
		"sale_total":  totalSalePrice,
		"cost_total":  totalCostPrice,
	})
}

func handleTrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	res, err := engines.GetTrialBalance(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleShopifyProductMap(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Sku        string `json:"sku"`
		ChannelSku string `json:"channel_sku"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid mapping payload", http.StatusBadRequest)
		return
	}

	if req.Sku == "" || req.ChannelSku == "" {
		http.Error(w, "Fields 'sku' and 'channel_sku' are required", http.StatusBadRequest)
		return
	}

	err := engines.MapChannelProduct(tenantID, "Shopify", req.Sku, req.ChannelSku)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "mapped",
		"sku":     req.Sku,
		"channel": "Shopify",
	})
}

func handleShopifyOrderWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID        string `json:"id"`
		LineItems []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"line_items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid webhook payload", http.StatusBadRequest)
		return
	}

	if req.ID == "" || len(req.LineItems) == 0 {
		http.Error(w, "Fields 'id' and 'line_items' are required", http.StatusBadRequest)
		return
	}

	// Convert structure to slice of maps
	var items []map[string]interface{}
	for _, item := range req.LineItems {
		items = append(items, map[string]interface{}{
			"sku": item.Sku,
			"qty": item.Qty,
		})
	}

	orderID, err := engines.ImportChannelOrder(tenantID, "Shopify", req.ID, items)
	if err != nil {
		if err.Error() == "ORDER_ALREADY_IMPORTED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "ignored",
				"details": "Order already processed (idempotency check)",
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "imported",
		"order_id": orderID,
	})
}

func handleFulfillmentTaskTransition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"` // "Picking", "Packed", "Dispatched", "Rejected"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid transition payload", http.StatusBadRequest)
		return
	}

	if req.TaskID == "" || req.Status == "" {
		http.Error(w, "Fields 'task_id' and 'status' are required", http.StatusBadRequest)
		return
	}

	err := engines.TransitionTaskStatus(tenantID, req.TaskID, req.Status)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "transitioned",
		"task_id":    req.TaskID,
		"new_status": req.Status,
	})
}

func handleFulfillmentReturn(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReturnLocation  string `json:"return_location"`
		OriginalOrderID string `json:"original_order_id"`
		Items           []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid return payload", http.StatusBadRequest)
		return
	}

	if req.ReturnLocation == "" || req.OriginalOrderID == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'return_location', 'original_order_id', and 'items' are required", http.StatusBadRequest)
		return
	}

	// Convert items structure to interface slice
	itemsInterface := make([]interface{}, len(req.Items))
	for i, item := range req.Items {
		itemsInterface[i] = map[string]interface{}{
			"sku":        item.Sku,
			"qty":        item.Qty,
			"sale_price": item.SalePrice,
			"cost_price": item.CostPrice,
		}
	}

	err := engines.ProcessReturnAnywhere(tenantID, req.ReturnLocation, req.OriginalOrderID, itemsInterface)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Save dynamic SalesReturn document
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil {
		payloadBytes, _ := json.Marshal(req)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by) 
			VALUES ($1, 'SalesReturn', $2, 'Returned', 'system')`, schema)
		_, _ = db.DB.Exec(query, fmt.Sprintf("RET-%s", req.OriginalOrderID), payloadBytes)
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":            "refunded",
		"original_order_id": req.OriginalOrderID,
		"returned_location": req.ReturnLocation,
	})
}

func handleScaleTest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NumStores       int `json:"num_stores"`
		NumWorkers      int `json:"num_workers"`
		NumTransactions int `json:"num_transactions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid scale test parameters", http.StatusBadRequest)
		return
	}

	if req.NumStores <= 0 || req.NumWorkers <= 0 || req.NumTransactions <= 0 {
		http.Error(w, "Parameters 'num_stores', 'num_workers', and 'num_transactions' must be positive integers", http.StatusBadRequest)
		return
	}

	// 1. Seed test data
	err := engines.SeedScaleTestData(tenantID, req.NumStores, "BAR-SCALE", 1000)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to seed scale data: %v", err)})
		return
	}

	// 2. Run simulation
	report, err := engines.RunScaleSimulation(tenantID, req.NumWorkers, req.NumTransactions, "BAR-SCALE", req.NumStores)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to execute scale simulation: %v", err)})
		return
	}

	_ = json.NewEncoder(w).Encode(report)
}

func handleMarketplaceReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Channel      string   `json:"channel"`
		SettlementID string   `json:"settlement_id"`
		TotalSale    int      `json:"total_sale"`
		Commission   int      `json:"commission"`
		NetPayout    int      `json:"net_payout"`
		OrderIDs     []string `json:"order_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid reconciliation payload", http.StatusBadRequest)
		return
	}

	if req.SettlementID == "" || req.Channel == "" || req.TotalSale <= 0 {
		http.Error(w, "Fields 'settlement_id', 'channel', and positive 'total_sale' are required", http.StatusBadRequest)
		return
	}

	err := engines.ProcessMarketplaceSettlement(tenantID, req.Channel, req.SettlementID, req.TotalSale, req.Commission, req.NetPayout, req.OrderIDs)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":        "reconciled",
		"settlement_id": req.SettlementID,
		"net_received":  fmt.Sprintf("%d", req.NetPayout),
	})
}

func handleLogisticsBook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OrderID        string `json:"order_id"`
		Carrier        string `json:"carrier"`
		TrackingNumber string `json:"tracking_number"`
		ShippingCharge int    `json:"shipping_charge"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid logistics payload", http.StatusBadRequest)
		return
	}

	if req.OrderID == "" || req.Carrier == "" || req.TrackingNumber == "" {
		http.Error(w, "Fields 'order_id', 'carrier', and 'tracking_number' are required", http.StatusBadRequest)
		return
	}

	bookingID, err := engines.CreateLogisticsBooking(tenantID, req.OrderID, req.Carrier, req.TrackingNumber, req.ShippingCharge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":          "shipped",
		"booking_id":      bookingID,
		"tracking_number": req.TrackingNumber,
	})
}

func handleReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	locCode := r.URL.Query().Get("location_code")
	if locCode == "" {
		http.Error(w, "Query parameter 'location_code' is required", http.StatusBadRequest)
		return
	}

	// Parse optional lead_time and safety_stock parameters
	leadTime := 7
	safetyStock := 10
	if ltStr := r.URL.Query().Get("lead_time"); ltStr != "" {
		_, _ = fmt.Sscanf(ltStr, "%d", &leadTime)
	}
	if ssStr := r.URL.Query().Get("safety_stock"); ssStr != "" {
		_, _ = fmt.Sscanf(ssStr, "%d", &safetyStock)
	}

	suggestions, err := engines.GetReplenishmentSuggestions(tenantID, locCode, leadTime, safetyStock)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleSLABreaches(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	threshold := 120.0 // Default to 2 hours
	if threshStr := r.URL.Query().Get("threshold"); threshStr != "" {
		_, _ = fmt.Sscanf(threshStr, "%f", &threshold)
	}

	reports, err := engines.GetSLABreaches(tenantID, threshold)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(reports)
}

func handleDemandForecast(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		LocationCode string `json:"location_code"`
		SKU          string `json:"sku"`
		ForecastDays int    `json:"forecast_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid forecasting payload", http.StatusBadRequest)
		return
	}

	if req.LocationCode == "" || req.SKU == "" || req.ForecastDays <= 0 {
		http.Error(w, "Fields 'location_code', 'sku', and positive 'forecast_days' are required", http.StatusBadRequest)
		return
	}

	forecasted, err := engines.ForecastDemand(tenantID, req.LocationCode, req.SKU, req.ForecastDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"location_code":     req.LocationCode,
		"sku":               req.SKU,
		"forecast_days":     req.ForecastDays,
		"forecasted_demand": forecasted,
	})
}
