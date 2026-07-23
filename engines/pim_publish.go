package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
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

	// Stage 26.4.7: channel validation packs - business rules beyond simple
	// field presence (minimum image count, title length, a required tag),
	// configured per channel via the ChannelValidationRule doctype. A no-op
	// for a channel with no rules configured (every pre-26.4.7 Channel).
	ruleFailures, err := evaluateChannelValidationRules(tenantID, itemCode, channelCode, defaultLocale)
	if err != nil {
		return nil, err
	}
	missing = append(missing, ruleFailures...)

	return &PublishReadiness{Ready: len(missing) == 0, MissingFields: missing}, nil
}

type channelValidationRule struct {
	RuleType  string
	RuleValue string
	Message   string
}

// fetchChannelValidationRules returns the Active ChannelValidationRule rows
// configured for a channel (Stage 26.4.7).
func fetchChannelValidationRules(tenantID, channelCode string) ([]channelValidationRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'rule_type', ''), COALESCE(data->>'rule_value', ''), COALESCE(data->>'message', '')
		FROM %s.documents WHERE doctype = 'ChannelValidationRule' AND data->>'channel' = $1 AND status = 'Active'`, schema), channelCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channelValidationRule
	for rows.Next() {
		var r channelValidationRule
		if err := rows.Scan(&r.RuleType, &r.RuleValue, &r.Message); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func channelRuleFailureMessage(rule channelValidationRule, detail string) string {
	if rule.Message != "" {
		return rule.Message
	}
	return fmt.Sprintf("%s: %s", rule.RuleType, detail)
}

func tagListContains(tags, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return true
	}
	for _, t := range strings.Split(tags, ",") {
		if strings.ToLower(strings.TrimSpace(t)) == target {
			return true
		}
	}
	return false
}

// evaluateChannelValidationRules checks an item's current media count/
// approved-content title+tags against a channel's configured validation
// pack, returning one human-readable failure message per broken rule (added
// straight into CheckPublishReadiness's existing missing-fields list, not a
// second parallel readiness concept).
func evaluateChannelValidationRules(tenantID, itemCode, channelCode, locale string) ([]string, error) {
	rules, err := fetchChannelValidationRules(tenantID, channelCode)
	if err != nil || len(rules) == 0 {
		return nil, err
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var title, tags string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'title', ''), COALESCE(data->>'tags', '')
		FROM %s.documents WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status = 'Approved'
		ORDER BY updated_at DESC LIMIT 1`, schema), itemCode, locale).Scan(&title, &tags)
	media, err := ListMediaForItem(tenantID, itemCode)
	if err != nil {
		return nil, err
	}

	var failures []string
	for _, rule := range rules {
		switch rule.RuleType {
		case "Min Images":
			minCount, _ := strconv.Atoi(rule.RuleValue)
			if len(media) < minCount {
				failures = append(failures, channelRuleFailureMessage(rule, fmt.Sprintf("requires at least %d image(s), item has %d", minCount, len(media))))
			}
		case "Max Title Length":
			maxLen, _ := strconv.Atoi(rule.RuleValue)
			if maxLen > 0 && len(title) > maxLen {
				failures = append(failures, channelRuleFailureMessage(rule, fmt.Sprintf("title is %d characters, exceeds the %d limit", len(title), maxLen)))
			}
		case "Required Tag":
			if !tagListContains(tags, rule.RuleValue) {
				failures = append(failures, channelRuleFailureMessage(rule, fmt.Sprintf("missing required tag %q", rule.RuleValue)))
			}
		}
	}
	return failures, nil
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
	ErrorCode    string `json:"error_code,omitempty"`
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
		SELECT job_id, channel_code, status, COALESCE(external_id, ''), COALESCE(error_message, ''), COALESCE(error_code, ''), created_at::text
		FROM %s.pim_publish_log WHERE item_code = $1 ORDER BY created_at DESC LIMIT 20`, schema), itemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PublishLogEntry
	for rows.Next() {
		var e PublishLogEntry
		if err := rows.Scan(&e.JobID, &e.ChannelCode, &e.Status, &e.ExternalID, &e.ErrorMessage, &e.ErrorCode, &e.CreatedAt); err != nil {
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
func StartPublishQueueWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
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

	tenantID, errTenant := tenantIDForSchema(schema)
	if errTenant != nil {
		log.Printf("[PIM-PUBLISH] Failed to resolve tenant for schema %s: %v", schema, errTenant)
		return
	}

	for _, j := range jobs {
		platform, _ := fetchChannelPlatform(tenantID, j.channelCode)
		connector := resolveConnector(platform)

		capacity, window := connector.RateLimit()
		if !allowConnectorCall(j.channelCode, capacity, window) {
			// Outbound budget exhausted for this channel right now - leave the
			// job Queued (not Failed) so the next tick retries it, exactly like
			// the existing retry_count/backoff shape already handles a real
			// platform error below.
			continue
		}

		externalID, payload, publishErr := publishOneJob(tenantID, schema, connector, j.itemCode, j.channelCode)
		// CONN-0225 (Stage 25.6): a circuit-breaker-open error is the same
		// shape as the allowConnectorCall rate-limit check just above - a
		// transient, this-platform-is-busy-right-now condition, not a real
		// per-job failure - so it gets the identical "leave Queued, don't
		// touch retry_count, try again next tick" treatment rather than
		// falling into the generic Failed branch below.
		if verr, ok := publishErr.(*ValidationError); ok && verr.Code == "CONN-0225" {
			continue
		}
		status := "Published"
		errMsg := ""
		errCode := ""
		if publishErr != nil {
			status = "Failed"
			errMsg = publishErr.Error()
			if verr, ok := publishErr.(*ValidationError); ok && verr.Code != "" {
				// Stage 26.4.8: now also recorded in its own error_code
				// column (not only ever embedded in the free-text message)
				// so a caller can filter/classify without string-parsing.
				errCode = verr.Code
				errMsg = "[" + verr.Code + "] " + errMsg
			}
			externalID = ""
		}
		var payloadSnapshot []byte
		if payload != nil {
			payloadSnapshot, _ = json.Marshal(payload)
		}

		retryIncrement := 0
		if status == "Failed" {
			retryIncrement = 1
		}
		if _, updErr := db.DB.Exec(fmt.Sprintf(`UPDATE %s.pim_publish_queue SET status = $1, retry_count = retry_count + $2, updated_at = CURRENT_TIMESTAMP WHERE job_id = $3`, schema), status, retryIncrement, j.id); updErr != nil {
			log.Printf("[PIM-PUBLISH] failed to update queue row for job %d: %v", j.id, updErr)
		}
		_, _ = db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.pim_publish_log (job_id, item_code, channel_code, status, external_id, error_message, error_code, payload_snapshot) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, schema),
			j.id, j.itemCode, j.channelCode, status, externalID, errMsg, errCode, payloadSnapshot)

		if tx, errTx := db.DB.Begin(); errTx == nil {
			_ = db.SetSearchPath(tx, schema)
			eventName := "pim.publish.published"
			if status != "Published" {
				eventName = "pim.publish.failed"
			}
			_ = PublishEvent(tx, schema, eventName, map[string]interface{}{
				"job_id": j.id, "item_code": j.itemCode, "channel_code": j.channelCode, "external_id": externalID, "platform": platform,
			})
			_ = tx.Commit()
		}

		advanceProfileToPublishOutcome(schema, j.itemCode, status)
		log.Printf("[PIM-PUBLISH] job %d: %s -> %s via %s (%s)", j.id, j.itemCode, j.channelCode, platform, status)
	}
}

// fetchChannelPlatform returns a Channel's platform field ("" if unset -
// resolveConnector falls back to the stub in that case).
func fetchChannelPlatform(tenantID, channelCode string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var platform string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'platform', '') FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channelCode).Scan(&platform)
	if err != nil {
		return "", err
	}
	return platform, nil
}

// publishOneJob builds the outbound payload, loads the channel's credential
// (best-effort - the stub connector ignores it entirely, so a missing
// credential is only a real problem once a real connector needs one, and
// that connector's own PublishProduct will report it as such), and calls
// the resolved connector with a bounded context. Separated from
// processPublishQueue so each attempt's panic/timeout safety (already
// handled one level down inside doConnectorRequest) has a clean boundary.
func publishOneJob(tenantID, schema string, connector ChannelConnector, itemCode, channelCode string) (externalID string, payload *ChannelProductPayload, err error) {
	payload, err = BuildChannelPayload(tenantID, itemCode, channelCode)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build channel payload: %v", err)
	}

	cred, credErr := getChannelCredential(tenantID, channelCode)
	if credErr != nil {
		cred = map[string]string{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	externalID, err = connector.PublishProduct(ctx, cred, *payload)
	return externalID, payload, err
}

// PreviewChannelDiff (Stage 26.4.7: "per-channel diff preview before
// publish") shows exactly what would be sent to a channel right now,
// field-by-field against the payload actually used in the last publish
// attempt for this item+channel (pim_publish_log.payload_snapshot, Stage
// 26.4.7) - not a live call to the platform to fetch its current state
// (which would mean adding a per-platform "read back" API call this
// framework doesn't otherwise need), a stated scope limit rather than a
// faked diff. HasPriorSnapshot is false the very first time an item is
// published to a channel, since no snapshot exists yet to diff against.
type ChannelDiffField struct {
	Field   string `json:"field"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Changed bool   `json:"changed"`
}

type ChannelDiffPreview struct {
	ItemCode         string             `json:"item_code"`
	ChannelCode      string             `json:"channel_code"`
	HasPriorSnapshot bool               `json:"has_prior_snapshot"`
	Fields           []ChannelDiffField `json:"fields"`
}

func PreviewChannelDiff(tenantID, itemCode, channelCode string) (*ChannelDiffPreview, error) {
	current, err := BuildChannelPayload(tenantID, itemCode, channelCode)
	if err != nil {
		return nil, err
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var snapshotStr string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT payload_snapshot::text FROM %s.pim_publish_log
		WHERE item_code = $1 AND channel_code = $2 AND payload_snapshot IS NOT NULL
		ORDER BY created_at DESC LIMIT 1`, schema), itemCode, channelCode).Scan(&snapshotStr)

	preview := &ChannelDiffPreview{ItemCode: itemCode, ChannelCode: channelCode}
	var prior ChannelProductPayload
	if err == nil {
		if unmarshalErr := json.Unmarshal([]byte(snapshotStr), &prior); unmarshalErr == nil {
			preview.HasPriorSnapshot = true
		}
	}

	addField := func(name, oldVal, newVal string) {
		preview.Fields = append(preview.Fields, ChannelDiffField{Field: name, Old: oldVal, New: newVal, Changed: oldVal != newVal})
	}
	addField("title", prior.Title, current.Title)
	addField("description", prior.Description, current.Description)
	addField("image_count", strconv.Itoa(len(prior.Images)), strconv.Itoa(len(current.Images)))

	attrKeys := map[string]bool{}
	for k := range prior.Attributes {
		attrKeys[k] = true
	}
	for k := range current.Attributes {
		attrKeys[k] = true
	}
	keys := make([]string, 0, len(attrKeys))
	for k := range attrKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		addField("attribute:"+k, prior.Attributes[k], current.Attributes[k])
	}

	return preview, nil
}
