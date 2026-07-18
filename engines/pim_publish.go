package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
)

// Channel Publishing (Stage 15.2, PIM Blueprint V2 §7/§11/§14). No real
// external channel credentials exist in this environment, so the
// "connector" here is an explicit stub: it marks a queued job Published
// with a fabricated STUB-<item>-<channel> external id rather than actually
// calling any Shopify/Amazon/OMS API. The queue/idempotency/retry/logging
// machinery around it is real and ready for a genuine connector to be
// dropped in later - stated as a real limitation, not hidden, matching
// this codebase's own existing scope-note conventions (e.g. sticker
// symbology, expense attachments). Events reuse the existing outbox
// (engines.PublishEvent, engines/outbox.go) rather than a new system.

type PublishReadiness struct {
	Ready         bool     `json:"ready"`
	MissingFields []string `json:"missing_fields"`
}

// CheckPublishReadiness validates an item is 100% complete for the
// channel's default_locale (including that channel's own mandatory field
// mappings, via CalculateCompleteness's channelID param) and has its
// category mapped, before allowing a publish to be queued.
func CheckPublishReadiness(tenantID, itemCode, channelCode string) (*PublishReadiness, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var defaultLocale string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'default_locale', 'en') FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channelCode).Scan(&defaultLocale); err != nil {
		return nil, fmt.Errorf("channel not found: %v", err)
	}
	if defaultLocale == "" {
		defaultLocale = "en"
	}

	comp, err := CalculateCompleteness(tenantID, itemCode, defaultLocale, channelCode)
	if err != nil {
		return nil, err
	}
	missing := append([]string{}, comp.MissingFields...)

	itemData, _, err := fetchItemDoc(tenantID, itemCode)
	if err != nil {
		return nil, err
	}
	if category, _ := itemData["category"].(string); category != "" {
		var mapCount int
		_ = db.DB.QueryRow(fmt.Sprintf(`
			SELECT COUNT(*) FROM %s.documents
			WHERE doctype = 'ChannelCategoryMap' AND data->>'channel' = $1 AND data->>'erp_category' = $2`, schema), channelCode, category).Scan(&mapCount)
		if mapCount == 0 {
			missing = append(missing, fmt.Sprintf("category mapping for %q on channel %q", category, channelCode))
		}
	}

	return &PublishReadiness{Ready: len(missing) == 0, MissingFields: missing}, nil
}

// computePublishPayloadHash hashes what would actually be published (the
// approved content for the channel's default locale) - the idempotency key
// (blueprint's "idempotency key required" rule): re-queuing the same
// unchanged item/channel is then a detectable no-op in QueuePublish.
func computePublishPayloadHash(tenantID, itemCode, channelCode string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var defaultLocale string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'default_locale', 'en') FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channelCode).Scan(&defaultLocale)
	if defaultLocale == "" {
		defaultLocale = "en"
	}
	var title, shortDesc, longDesc string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'title', ''), COALESCE(data->>'short_desc', ''), COALESCE(data->>'long_desc', '')
		FROM %s.documents WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status = 'Approved'
		ORDER BY updated_at DESC LIMIT 1`, schema), itemCode, defaultLocale).Scan(&title, &shortDesc, &longDesc)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s", itemCode, channelCode, title, shortDesc, longDesc)))
	return hex.EncodeToString(sum[:])
}

// QueuePublish validates readiness and inserts a pim_publish_queue row.
// Idempotent: if an unchanged item/channel/content combination (same
// payload_hash) already has a Queued/Processing/Published job, no new row
// is inserted - re-queuing an unchanged item is a detected no-op rather
// than a duplicate listing.
func QueuePublish(tenantID, itemCode, channelCode, actorUserID string) (jobID int, alreadyQueued bool, err error) {
	readiness, err := CheckPublishReadiness(tenantID, itemCode, channelCode)
	if err != nil {
		return 0, false, err
	}
	if !readiness.Ready {
		return 0, false, fmt.Errorf("item is not ready to publish to %s: %s", channelCode, strings.Join(readiness.MissingFields, "; "))
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false, err
	}
	payloadHash := computePublishPayloadHash(tenantID, itemCode, channelCode)

	var existingID int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT job_id FROM %s.pim_publish_queue
		WHERE item_code = $1 AND channel_code = $2 AND payload_hash = $3 AND status IN ('Queued','Processing','Published')
		ORDER BY job_id DESC LIMIT 1`, schema), itemCode, channelCode, payloadHash).Scan(&existingID)
	if err == nil {
		return existingID, true, nil
	}

	var newJobID int
	err = db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.pim_publish_queue (item_code, channel_code, payload_hash, status)
		VALUES ($1, $2, $3, 'Queued') RETURNING job_id`, schema), itemCode, channelCode, payloadHash).Scan(&newJobID)
	if err != nil {
		return 0, false, err
	}

	if tx, errTx := db.DB.Begin(); errTx == nil {
		_ = db.SetSearchPath(tx, schema)
		_ = PublishEvent(tx, schema, "pim.publish.queued", map[string]interface{}{
			"job_id": newJobID, "item_code": itemCode, "channel_code": channelCode, "actor": actorUserID,
		})
		_ = tx.Commit()
	}

	return newJobID, false, nil
}

type PublishJobStatus struct {
	JobID        int    `json:"job_id"`
	ItemCode     string `json:"item_code"`
	ChannelCode  string `json:"channel_code"`
	Status       string `json:"status"`
	RetryCount   int    `json:"retry_count"`
	ExternalID   string `json:"external_id,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func GetPublishJobStatus(tenantID string, jobID int) (*PublishJobStatus, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	s := &PublishJobStatus{JobID: jobID}
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT item_code, channel_code, status, retry_count FROM %s.pim_publish_queue WHERE job_id = $1`, schema), jobID).
		Scan(&s.ItemCode, &s.ChannelCode, &s.Status, &s.RetryCount)
	if err != nil {
		return nil, fmt.Errorf("publish job not found: %v", err)
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(external_id, ''), COALESCE(error_message, '') FROM %s.pim_publish_log WHERE job_id = $1 ORDER BY created_at DESC LIMIT 1`, schema), jobID).
		Scan(&s.ExternalID, &s.ErrorMessage)
	return s, nil
}

type PublishLogEntry struct {
	JobID        int    `json:"job_id"`
	ChannelCode  string `json:"channel_code"`
	Status       string `json:"status"`
	ExternalID   string `json:"external_id"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
}

// ListPublishLogForItem returns the most recent publish attempts for an
// item across all channels, for the Workbench detail panel.
func ListPublishLogForItem(tenantID, itemCode string) ([]PublishLogEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT job_id, channel_code, status, COALESCE(external_id, ''), COALESCE(error_message, ''), created_at::text
		FROM %s.pim_publish_log WHERE item_code = $1 ORDER BY created_at DESC LIMIT 20`, schema), itemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PublishLogEntry
	for rows.Next() {
		var e PublishLogEntry
		if err := rows.Scan(&e.JobID, &e.ChannelCode, &e.Status, &e.ExternalID, &e.ErrorMessage, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// advanceProfileToPublishOutcome is the one place allowed to write a
// publishOwnedStatuses value directly (bypassing deriveAndPersistProfileStatus's
// normal derive-from-completeness path), since it reflects a real publish
// attempt outcome rather than a routine completeness recompute. Takes
// schema directly (not tenantID) since processPublishQueue only has schema,
// same as processOutbox's own shape (engines/outbox.go).
func advanceProfileToPublishOutcome(schema, itemCode, publishStatus string) {
	profileStatus := "Publish Failed"
	if publishStatus == "Published" {
		profileStatus = "Published"
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents
		SET data = jsonb_set(data, '{enrichment_status}', to_jsonb($1::text)), status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'PIMProductProfile' AND id = $2`, schema), profileStatus, pimProductProfileID(itemCode))
}

// StartPublishQueueWorker starts a background worker that processes Queued
// pim_publish_queue rows across every provisioned tenant schema - mirrors
// StartOutboxWorker's exact shape (engines/outbox.go).
func StartPublishQueueWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}
			schemas, err := listTenantSchemas()
			if err != nil {
				log.Printf("[PIM-PUBLISH] Failed to list tenant schemas: %v", err)
				continue
			}
			for _, schema := range schemas {
				processPublishQueue(schema)
			}
		}
	}()
}

func processPublishQueue(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT job_id, item_code, channel_code FROM %s.pim_publish_queue
		WHERE status = 'Queued' ORDER BY created_at LIMIT 10`, schema))
	if err != nil {
		return
	}
	type job struct {
		id          int
		itemCode    string
		channelCode string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.itemCode, &j.channelCode); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, j := range jobs {
		// Stub connector - see file header note. Always "succeeds" with a
		// fabricated external id; a real connector call would replace this
		// block.
		externalID := fmt.Sprintf("STUB-%s-%s", j.itemCode, j.channelCode)
		status := "Published"

		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.pim_publish_queue SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE job_id = $2`, schema), status, j.id)
		_, _ = db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.pim_publish_log (job_id, item_code, channel_code, status, external_id) VALUES ($1, $2, $3, $4, $5)`, schema),
			j.id, j.itemCode, j.channelCode, status, externalID)

		if tx, errTx := db.DB.Begin(); errTx == nil {
			_ = db.SetSearchPath(tx, schema)
			eventName := "pim.publish.published"
			if status != "Published" {
				eventName = "pim.publish.failed"
			}
			_ = PublishEvent(tx, schema, eventName, map[string]interface{}{
				"job_id": j.id, "item_code": j.itemCode, "channel_code": j.channelCode, "external_id": externalID,
			})
			_ = tx.Commit()
		}

		advanceProfileToPublishOutcome(schema, j.itemCode, status)
		log.Printf("[PIM-PUBLISH] job %d: %s -> %s (%s)", j.id, j.itemCode, j.channelCode, status)
	}
}
