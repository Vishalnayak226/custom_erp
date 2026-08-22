package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

type ChannelRuntimeConfig struct {
	Code                string `json:"code"`
	Platform            string `json:"platform"`
	Kind                string `json:"kind"`
	LocationCode        string `json:"location_code"`
	NoSplit             bool   `json:"no_split"`
	InventoryBuffer     int    `json:"inventory_buffer"`
	SyncIntervalMinutes int    `json:"sync_interval_minutes"`
	Status              string `json:"status"`
}
type ChannelSKUMapping struct {
	SKU               string    `json:"sku"`
	Channel           string    `json:"channel"`
	ChannelSKU        string    `json:"channel_sku"`
	ExternalProductID string    `json:"external_product_id"`
	LocationCode      string    `json:"location_code"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type ChannelHealth struct {
	Channel        string  `json:"channel"`
	Platform       string  `json:"platform"`
	LastSyncAt     string  `json:"last_sync_at"`
	LastStatus     string  `json:"last_status"`
	LastError      string  `json:"last_error"`
	LagSeconds     int64   `json:"lag_seconds"`
	Runs24h        int     `json:"runs_24h"`
	Failures24h    int     `json:"failures_24h"`
	FailureRate    float64 `json:"failure_rate"`
	OpenExceptions int     `json:"open_exceptions"`
	CanPullOrders  bool    `json:"can_pull_orders"`
	CanPushATS     bool    `json:"can_push_ats"`
}

type ChannelSKUException struct {
	ID           string `json:"id"`
	Channel      string `json:"channel"`
	ChannelSKU   string `json:"channel_sku"`
	FirstOrderID string `json:"first_order_id"`
	LastOrderID  string `json:"last_order_id"`
	Occurrences  int    `json:"occurrences"`
	CreatedAt    string `json:"created_at"`
}

func channelBool(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true") || strings.TrimSpace(value) == "1"
	case float64:
		return value != 0
	}
	return false
}

func LoadChannelRuntimeConfig(tenantID, channelCode string) (ChannelRuntimeConfig, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ChannelRuntimeConfig{}, err
	}
	var raw []byte
	var status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data,status FROM %s.documents WHERE doctype='Channel' AND id=$1 AND deleted_at IS NULL`, schema), channelCode).Scan(&raw, &status)
	if err != nil {
		return ChannelRuntimeConfig{}, fmt.Errorf("channel %s not found", channelCode)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return ChannelRuntimeConfig{}, err
	}
	cfg := ChannelRuntimeConfig{Code: channelCode, Platform: strings.TrimSpace(fmt.Sprint(data["platform"])), Kind: strings.TrimSpace(fmt.Sprint(data["connector_kind"])), LocationCode: strings.TrimSpace(fmt.Sprint(data["location_code"])), Status: status}
	if cfg.Platform == "" || cfg.Platform == "<nil>" {
		cfg.Platform = channelCode
	}
	if cfg.Kind == "<nil>" {
		cfg.Kind = ""
	}
	if cfg.LocationCode == "<nil>" {
		cfg.LocationCode = ""
	}
	cfg.NoSplit = channelBool(data["no_split"])
	cfg.InventoryBuffer = int(numericFromAny(data["inventory_buffer"]))
	cfg.SyncIntervalMinutes = int(numericFromAny(data["sync_interval_minutes"]))
	if cfg.SyncIntervalMinutes <= 0 {
		cfg.SyncIntervalMinutes = 15
	}
	return cfg, nil
}

func UpsertChannelSKUMapping(tenantID string, m ChannelSKUMapping) error {
	if m.SKU == "" || m.Channel == "" || m.ChannelSKU == "" {
		return fmt.Errorf("sku, channel and channel_sku are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.channel_product_mapping (sku,channel,channel_sku,external_product_id,location_code,updated_at) VALUES ($1,$2,$3,$4,$5,CURRENT_TIMESTAMP) ON CONFLICT (sku,channel) DO UPDATE SET channel_sku=EXCLUDED.channel_sku,external_product_id=EXCLUDED.external_product_id,location_code=EXCLUDED.location_code,updated_at=CURRENT_TIMESTAMP`, schema), m.SKU, m.Channel, m.ChannelSKU, m.ExternalProductID, m.LocationCode)
	if err == nil {
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status='Resolved',data=data || jsonb_build_object('status','Resolved','resolved_sku',$1::text,'resolved_at',$2::text),updated_at=CURRENT_TIMESTAMP WHERE doctype='ChannelSKUException' AND data->>'channel'=$3 AND data->>'channel_sku'=$4 AND status='Open'`, schema), m.SKU, time.Now().UTC().Format(time.RFC3339), m.Channel, m.ChannelSKU)
	}
	return err
}
func ListChannelSKUMappings(tenantID, channel string) ([]ChannelSKUMapping, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku,channel,channel_sku,COALESCE(external_product_id,''),COALESCE(location_code,''),updated_at FROM %s.channel_product_mapping WHERE ($1='' OR channel=$1) ORDER BY channel,sku`, schema), channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelSKUMapping
	for rows.Next() {
		var m ChannelSKUMapping
		if err := rows.Scan(&m.SKU, &m.Channel, &m.ChannelSKU, &m.ExternalProductID, &m.LocationCode, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func RecordChannelSKUException(tenantID, channel, channelSKU, channelOrderID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype='ChannelSKUException' AND data->>'channel'=$1 AND data->>'channel_sku'=$2 AND status='Open' AND deleted_at IS NULL LIMIT 1`, schema), channel, channelSKU).Scan(&id)
	if err == nil {
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data=jsonb_set(jsonb_set(data,'{occurrences}',to_jsonb(COALESCE((data->>'occurrences')::int,1)+1)),'{last_order_id}',to_jsonb($1::text)),updated_at=CURRENT_TIMESTAMP WHERE id=$2`, schema), channelOrderID, id)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	id = NewDocID("CSX")
	body, _ := json.Marshal(map[string]interface{}{"code": id, "channel": channel, "channel_sku": channelSKU, "first_order_id": channelOrderID, "last_order_id": channelOrderID, "occurrences": 1, "status": "Open"})
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id,doctype,data,status,created_by) VALUES($1,'ChannelSKUException',$2,'Open','system')`, schema), id, body)
	return err
}

func ListOpenChannelSKUExceptions(tenantID, channel string) ([]ChannelSKUException, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id,COALESCE(data->>'channel',''),COALESCE(data->>'channel_sku',''),COALESCE(data->>'first_order_id',''),COALESCE(data->>'last_order_id',''),CASE WHEN COALESCE(data->>'occurrences','') ~ '^\d+$' THEN (data->>'occurrences')::int ELSE 1 END,created_at::text FROM %s.documents WHERE doctype='ChannelSKUException' AND status='Open' AND deleted_at IS NULL AND ($1='' OR data->>'channel'=$1) ORDER BY updated_at DESC`, schema), channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSKUException{}
	for rows.Next() {
		var item ChannelSKUException
		if err := rows.Scan(&item.ID, &item.Channel, &item.ChannelSKU, &item.FirstOrderID, &item.LastOrderID, &item.Occurrences, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func recordChannelSyncRun(tenantID, channel, operation, status string, processed, failed int, started time.Time, runErr error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	id := NewDocID("CSR")
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	body, _ := json.Marshal(map[string]interface{}{"code": id, "channel": channel, "operation": operation, "status": status, "processed": processed, "failed": failed, "error": message, "started_at": started.UTC().Format(time.RFC3339), "finished_at": time.Now().UTC().Format(time.RFC3339)})
	_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id,doctype,data,status,created_by) VALUES($1,'ChannelSyncRun',$2,$3,'system')`, schema), id, body, status)
}

func PullChannelOrders(ctx context.Context, tenantID, channelCode string) (processed, failed int, err error) {
	started := time.Now()
	status := "Success"
	defer func() {
		if err != nil {
			status = "Failed"
		}
		recordChannelSyncRun(tenantID, channelCode, "Order Pull", status, processed, failed, started, err)
	}()
	cfg, err := LoadChannelRuntimeConfig(tenantID, channelCode)
	if err != nil {
		return 0, 0, err
	}
	connector, err := ResolveOmnichannelConnector(cfg.Platform)
	if err != nil {
		return 0, 0, err
	}
	cred, err := LoadConnectorCredential(tenantID, channelCode, cfg.Platform)
	if err != nil {
		return 0, 0, err
	}
	schema, _ := db.GetTenantSchema(tenantID)
	cursor := ""
	cursorStarted := ""
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'last_cursor',''),COALESCE(data->>'last_cursor_started_at','') FROM %s.documents WHERE doctype='Channel' AND id=$1`, schema), channelCode).Scan(&cursor, &cursorStarted)
	updatedAfter := time.Now().UTC().Add(-24 * time.Hour)
	if cursor != "" && cursorStarted != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, cursorStarted); parseErr == nil {
			updatedAfter = parsed
		}
	}
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		page, pullErr := connector.PullOrders(ctx, cred, ConnectorPullRequest{UpdatedAfter: updatedAfter, Cursor: cursor, Limit: 100})
		if pullErr != nil {
			return processed, failed, pullErr
		}
		for _, o := range page.Orders {
			lines := make([]SalesOrderLineInput, 0, len(o.Lines))
			for _, l := range o.Lines {
				lines = append(lines, SalesOrderLineInput{SKU: l.ChannelSKU, Qty: l.Quantity, UnitPrice: l.UnitPrice})
			}
			location := o.LocationCode
			if location == "" {
				location = cfg.LocationCode
			}
			_, importErr := ImportChannelSalesOrder(tenantID, ChannelOrderInput{Channel: channelCode, ChannelOrderID: o.ChannelOrderID, CustomerName: o.CustomerName, CustomerPhone: o.CustomerPhone, ShippingAddress: o.ShippingAddress, ShippingState: o.ShippingState, PaymentStatus: o.PaymentStatus, PreferredLocation: location, NoSplit: cfg.NoSplit || connector.Descriptor().NoSplit, Lines: lines})
			if importErr != nil {
				failed++
				continue
			}
			processed++
		}
		cursor = page.NextCursor
		_, cursorErr := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data=data || jsonb_build_object('last_cursor',$1::text,'last_cursor_started_at',$2::text),updated_at=CURRENT_TIMESTAMP WHERE doctype='Channel' AND id=$3`, schema), cursor, func() string {
			if cursor == "" {
				return ""
			}
			return updatedAfter.Format(time.RFC3339)
		}(), channelCode)
		if cursorErr != nil {
			return processed, failed, cursorErr
		}
		if cursor == "" {
			return processed, failed, nil
		}
	}
	return processed, failed, fmt.Errorf("channel %s exceeded 100 order pages in one sync", channelCode)
}

// RefreshChannelBufferGuard writes the most conservative active channel
// buffer into the existing ATS term. Per-channel pushes may subtract more in
// future; they must never expose more than this system-wide oversell guard.
func RefreshChannelBufferGuard(tenantID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var buffer int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(CASE WHEN COALESCE(data->>'inventory_buffer','') ~ '^\d+$' THEN (data->>'inventory_buffer')::int ELSE 0 END),0) FROM %s.documents WHERE doctype='Channel' AND status='Active' AND deleted_at IS NULL`, schema)).Scan(&buffer)
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET channel_buffer=$1,updated_at=CURRENT_TIMESTAMP WHERE channel_buffer<>$1`, schema), buffer)
	return err
}

func SyncChannelInventory(ctx context.Context, tenantID, channelCode string) (processed, failed int, err error) {
	started := time.Now()
	status := "Success"
	defer func() {
		if err != nil {
			status = "Failed"
		}
		recordChannelSyncRun(tenantID, channelCode, "Inventory Push", status, processed, failed, started, err)
	}()
	cfg, err := LoadChannelRuntimeConfig(tenantID, channelCode)
	if err != nil {
		return 0, 0, err
	}
	connector, err := ResolveOmnichannelConnector(cfg.Platform)
	if err != nil {
		return 0, 0, err
	}
	cred, err := LoadConnectorCredential(tenantID, channelCode, cfg.Platform)
	if err != nil {
		return 0, 0, err
	}
	if err = RefreshChannelBufferGuard(tenantID); err != nil {
		return 0, 0, err
	}
	mappings, err := ListChannelSKUMappings(tenantID, channelCode)
	if err != nil {
		return 0, 0, err
	}
	updates := make([]ConnectorInventoryUpdate, 0, len(mappings))
	for _, m := range mappings {
		location := m.LocationCode
		if location == "" {
			location = cfg.LocationCode
		}
		quantity, quantityErr := ComputeSellableSKUATS(tenantID, m.SKU, location)
		if quantityErr != nil {
			return processed, failed, quantityErr
		}
		if quantity < 0 {
			quantity = 0
		}
		updates = append(updates, ConnectorInventoryUpdate{ChannelSKU: m.ChannelSKU, Quantity: quantity, LocationCode: location, ProductID: m.ExternalProductID})
	}
	if len(updates) == 0 {
		return 0, 0, nil
	}
	if err = connector.PushInventory(ctx, cred, updates); err != nil {
		return 0, len(updates), err
	}
	return len(updates), 0, nil
}

func PushChannelOrderStatus(ctx context.Context, tenantID, channelCode string, u ConnectorStatusUpdate) error {
	cfg, err := LoadChannelRuntimeConfig(tenantID, channelCode)
	if err != nil {
		return err
	}
	connector, err := ResolveOmnichannelConnector(cfg.Platform)
	if err != nil {
		return err
	}
	cred, err := LoadConnectorCredential(tenantID, channelCode, cfg.Platform)
	if err != nil {
		return err
	}
	started := time.Now()
	err = connector.PushOrderStatus(ctx, cred, u)
	status := "Success"
	if err != nil {
		status = "Failed"
	}
	recordChannelSyncRun(tenantID, channelCode, "Status Push", status, 1, func() int {
		if err != nil {
			return 1
		}
		return 0
	}(), started, err)
	return err
}

func GetConnectorHealth(tenantID string) ([]ChannelHealth, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id,COALESCE(data->>'platform',id) FROM %s.documents WHERE doctype='Channel' AND status='Active' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	var out []ChannelHealth
	for rows.Next() {
		var h ChannelHealth
		if err := rows.Scan(&h.Channel, &h.Platform); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		h := &out[i]
		if connector, resolveErr := ResolveOmnichannelConnector(h.Platform); resolveErr == nil {
			descriptor := connector.Descriptor()
			h.CanPullOrders = descriptor.PullOrders
			h.CanPushATS = descriptor.PushInventory
		}
		var finished sql.NullString
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'finished_at',status,COALESCE(data->>'error','') FROM %s.documents WHERE doctype='ChannelSyncRun' AND data->>'channel'=$1 ORDER BY created_at DESC LIMIT 1`, schema), h.Channel).Scan(&finished, &h.LastStatus, &h.LastError)
		if finished.Valid {
			h.LastSyncAt = finished.String
			if t, e := time.Parse(time.RFC3339, finished.String); e == nil {
				h.LagSeconds = int64(time.Since(t).Seconds())
			}
		}
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*),COUNT(*) FILTER(WHERE status='Failed') FROM %s.documents WHERE doctype='ChannelSyncRun' AND data->>'channel'=$1 AND created_at>=CURRENT_TIMESTAMP-INTERVAL '24 hours'`, schema), h.Channel).Scan(&h.Runs24h, &h.Failures24h)
		if h.Runs24h > 0 {
			h.FailureRate = float64(h.Failures24h) / float64(h.Runs24h)
		}
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='ChannelSKUException' AND data->>'channel'=$1 AND status='Open'`, schema), h.Channel).Scan(&h.OpenExceptions)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}

func RunScheduledChannelSyncs(ctx context.Context, tenantID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype='Channel' AND status='Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		return
	}
	var channels []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			channels = append(channels, c)
		}
	}
	rows.Close()
	for _, channel := range channels {
		cfg, cfgErr := LoadChannelRuntimeConfig(tenantID, channel)
		if cfgErr != nil {
			continue
		}
		connector, resolveErr := ResolveOmnichannelConnector(cfg.Platform)
		if resolveErr != nil {
			continue
		}
		var lastRun sql.NullTime
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT MAX(created_at) FROM %s.documents WHERE doctype='ChannelSyncRun' AND data->>'channel'=$1`, schema), channel).Scan(&lastRun)
		if lastRun.Valid && time.Since(lastRun.Time) < time.Duration(cfg.SyncIntervalMinutes)*time.Minute {
			continue
		}
		capabilities := connector.Descriptor()
		if capabilities.PullOrders {
			_, _, _ = PullChannelOrders(ctx, tenantID, channel)
		}
		if capabilities.PushInventory {
			_, _, _ = SyncChannelInventory(ctx, tenantID, channel)
		}
	}
}
func StartChannelSyncWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[CHANNEL-SYNC] list tenants: %v", err)
					continue
				}
				for _, schema := range schemas {
					RunScheduledChannelSyncs(ctx, schemaToTenantID(schema))
				}
			}
		}
	}()
}
