package engines

import (
	"bytes"
	"custom_erp/db"
	"database/sql"
	"encoding/csv"
	"fmt"
	"strconv"
)

// PIMDashboard is the compact catalog-health snapshot used by the PIM
// dashboard. Its values are deliberately counts, rather than a second
// derived-state table: PIMProductProfile is already the write-through
// snapshot for completeness and enrichment status, while publish jobs and
// media remain authoritative in their dedicated tables/documents.
type PIMDashboard struct {
	TotalProducts           int `json:"total_products"`
	IncompleteProducts      int `json:"incomplete_products"`
	PendingContentApprovals int `json:"pending_content_approvals"`
	ReadyToPublish          int `json:"ready_to_publish"`
	PublishedProducts       int `json:"published_products"`
	MissingMainImages       int `json:"missing_main_images"`
	QueuedPublishJobs       int `json:"queued_publish_jobs"`
	FailedPublishJobs       int `json:"failed_publish_jobs"`
}

// ListPIMReport returns one allowlisted, read-only PIM quality report. The
// row maps keep the HTTP/UI layer generic while the SQL remains explicit and
// reviewable rather than accepting a caller-provided query or identifier.
func ListPIMReport(tenantID, name string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	queries := map[string]string{
		"content-aging":       fmt.Sprintf(`SELECT id AS content_id, COALESCE(data->>'product_id','') AS item_code, COALESCE(data->>'language','') AS language, status, EXTRACT(DAY FROM CURRENT_TIMESTAMP - updated_at)::int AS age_days FROM %s.documents WHERE doctype = 'ProductContent' AND status IN ('Draft','Pending Approval','Rejected') ORDER BY updated_at ASC`, schema),
		"duplicate-media":     fmt.Sprintf(`SELECT COALESCE(data->>'checksum','') AS checksum, COUNT(*)::int AS media_count, string_agg(COALESCE(data->>'item',''), ', ' ORDER BY data->>'item') AS items FROM %s.documents WHERE doctype = 'ProductMedia' AND status = 'Active' AND COALESCE(data->>'checksum','') <> '' GROUP BY data->>'checksum' HAVING COUNT(*) > 1 ORDER BY media_count DESC, checksum`, schema),
		"channel-mapping-gap": fmt.Sprintf(`SELECT channel.id AS channel_code, item.id AS item_code, COALESCE(item.data->>'category','') AS erp_category FROM %s.documents channel CROSS JOIN %s.documents item WHERE channel.doctype = 'Channel' AND item.doctype = 'Item' AND COALESCE(item.data->>'category','') <> '' AND NOT EXISTS (SELECT 1 FROM %s.documents mapping WHERE mapping.doctype = 'ChannelCategoryMap' AND mapping.data->>'channel' = channel.id AND mapping.data->>'erp_category' = item.data->>'category') ORDER BY channel.id, item.id`, schema, schema, schema),
		"attribute-quality":   fmt.Sprintf(`SELECT id AS attribute_value_id, COALESCE(data->>'item','') AS item_code, COALESCE(data->>'attribute','') AS attribute_code, COALESCE(data->>'value','') AS value, status FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND (BTRIM(COALESCE(data->>'value','')) = '' OR status <> 'Active') ORDER BY item_code, attribute_code`, schema),
		// Stage 26.4.4: media whose expiry_date has passed (or has none set
		// alongside a role that usually needs one, e.g. Certificate) but is
		// still Active - a governance gap, not caught by anything above.
		"media-expiry": fmt.Sprintf(`SELECT id AS media_id, COALESCE(data->>'item','') AS item_code, COALESCE(data->>'media_role','') AS media_role, COALESCE(data->>'expiry_date','') AS expiry_date FROM %s.documents WHERE doctype = 'ProductMedia' AND status = 'Active' AND COALESCE(data->>'expiry_date','') <> '' AND NULLIF(data->>'expiry_date','')::date <= CURRENT_DATE ORDER BY (data->>'expiry_date')::date ASC`, schema),
		// Stage 26.4.5: content past its owner-set SLA due date that still
		// hasn't reached Approved - a content-workflow health check, same
		// shape as content-aging above but keyed off the SLA field instead
		// of raw age.
		"content-sla-breach": fmt.Sprintf(`SELECT id AS content_id, COALESCE(data->>'product_id','') AS item_code, COALESCE(data->>'owner','') AS owner, status, COALESCE(data->>'sla_due_date','') AS sla_due_date FROM %s.documents WHERE doctype = 'ProductContent' AND status NOT IN ('Approved') AND COALESCE(data->>'sla_due_date','') <> '' AND NULLIF(data->>'sla_due_date','')::date < CURRENT_DATE ORDER BY (data->>'sla_due_date')::date ASC`, schema),
	}
	query, ok := queries[name]
	if !ok {
		return nil, fmt.Errorf("unknown PIM report %q", name)
	}
	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]interface{}{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		destinations := make([]interface{}, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		row := map[string]interface{}{}
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				row[columns[i]] = string(bytes)
			} else if value == nil {
				row[columns[i]] = ""
			} else {
				row[columns[i]] = value
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return result, nil
}

// GetPIMDashboard returns the current tenant's eight PIM catalog-health
// counters. PIMProductProfile is intentionally the source for product and
// enrichment counters: it is the existing per-Item snapshot maintained by
// the completeness/publish workflows, so reading the dashboard never causes
// an expensive rescore or mutates product state.
func GetPIMDashboard(tenantID string) (*PIMDashboard, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	dashboard := &PIMDashboard{}
	err = db.DB.QueryRow(fmt.Sprintf(`
		WITH profiles AS (
			SELECT
				COALESCE(data->>'product_id', '') AS product_id,
				COALESCE(NULLIF(data->>'completeness_score', '')::numeric, 0) AS completeness_score,
				COALESCE(data->>'enrichment_status', '') AS enrichment_status
			FROM %s.documents
			WHERE doctype = 'PIMProductProfile'
		)
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE completeness_score < 100)::int,
			(SELECT COUNT(*)::int FROM %s.documents
				WHERE doctype = 'ProductContent' AND status = 'Pending Approval'),
			COUNT(*) FILTER (WHERE enrichment_status = 'Ready to Publish')::int,
			COUNT(*) FILTER (WHERE enrichment_status = 'Published')::int,
			COUNT(*) FILTER (WHERE NOT EXISTS (
				SELECT 1 FROM %s.documents media
				WHERE media.doctype = 'ProductMedia'
					AND media.status = 'Active'
					AND media.data->>'item' = profiles.product_id
					AND media.data->>'media_role' = 'Main Image'
			))::int,
			(SELECT COUNT(*)::int FROM %s.pim_publish_queue
				WHERE status IN ('Queued', 'Processing')),
			(SELECT COUNT(*)::int FROM %s.pim_publish_queue WHERE status = 'Failed')
		FROM profiles`, schema, schema, schema, schema, schema)).Scan(
		&dashboard.TotalProducts,
		&dashboard.IncompleteProducts,
		&dashboard.PendingContentApprovals,
		&dashboard.ReadyToPublish,
		&dashboard.PublishedProducts,
		&dashboard.MissingMainImages,
		&dashboard.QueuedPublishJobs,
		&dashboard.FailedPublishJobs,
	)
	if err != nil {
		return nil, err
	}
	return dashboard, nil
}

// GetSearchFeedExportCSV (Stage 26.4.9) is a read-only export of existing
// PIM data shaped for feeding an external search/discovery index (title,
// description, tags as facets, family/category, a ranking hint via the
// completeness score, and whether a main image exists) - a report, not a
// search engine of its own, same "reuse existing patterns" boundary the
// PDF's own §6.1 note draws. Scoped to the "en" default locale content only,
// same simplification BuildChannelPayload/CalculateCompleteness already
// make elsewhere for a single-locale-per-call shape.
func GetSearchFeedExportCSV(tenantID string) ([]byte, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT
			item.id AS item_code,
			COALESCE(item.data->>'name','') AS name,
			COALESCE(content.data->>'title','') AS title,
			COALESCE(content.data->>'short_desc','') AS short_desc,
			COALESCE(content.data->>'tags','') AS tags,
			COALESCE(item.data->>'family','') AS family,
			COALESCE(item.data->>'category','') AS category,
			COALESCE(NULLIF(profile.data->>'completeness_score','')::numeric, 0) AS completeness_score,
			EXISTS (
				SELECT 1 FROM %s.documents media
				WHERE media.doctype = 'ProductMedia' AND media.status = 'Active'
					AND media.data->>'item' = item.id AND media.data->>'media_role' = 'Main Image'
			) AS has_main_image
		FROM %s.documents item
		LEFT JOIN %s.documents profile ON profile.doctype = 'PIMProductProfile' AND profile.id = item.id || '::profile'
		LEFT JOIN %s.documents content ON content.doctype = 'ProductContent' AND content.data->>'product_id' = item.id AND content.data->>'language' = 'en' AND content.status = 'Approved'
		WHERE item.doctype = 'Item' AND item.status != 'Cancelled'
		ORDER BY item.id`, schema, schema, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"item_code", "name", "title", "short_desc", "tags", "family", "category", "completeness_score", "has_main_image"})
	for rows.Next() {
		var itemCode, name, title, shortDesc, tags, family, category string
		var score float64
		var hasMainImage bool
		if err := rows.Scan(&itemCode, &name, &title, &shortDesc, &tags, &family, &category, &score, &hasMainImage); err != nil {
			return nil, err
		}
		_ = writer.Write([]string{itemCode, name, title, shortDesc, tags, family, category, strconv.FormatFloat(score, 'f', 1, 64), strconv.FormatBool(hasMainImage)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	writer.Flush()
	return buf.Bytes(), writer.Error()
}
