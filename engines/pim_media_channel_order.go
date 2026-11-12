package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Stage 146.5.9: image positions per channel. A ProductMedia row keeps
// data.channel_positions = {"<channel>": <position>}; a channel with no
// positions on an item gets today's order (ListMediaForItem: by id), so
// nothing changes until a person orders a channel's images. Positioned
// images come first, in position order, then the rest in today's order.

const channelOrderCode = "PIM-9152"

// OrderMediaForChannel puts the positioned media first (position, then the
// incoming order) and leaves the rest in the incoming order. No positions
// returns the list as it came.
func OrderMediaForChannel(media []ProductMediaAsset, pos map[string]int) []ProductMediaAsset {
	if len(pos) == 0 {
		return media
	}
	out := append([]ProductMediaAsset(nil), media...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, iok := pos[out[i].ID]
		pj, jok := pos[out[j].ID]
		if iok != jok {
			return iok
		}
		return iok && pi < pj
	})
	return out
}

// channelImagePositions is media id -> position for one item and channel.
func channelImagePositions(tenantID, item, channel string) (map[string]int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data->'channel_positions'->>$2 FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND status = 'Active' AND deleted_at IS NULL
		  AND data->'channel_positions'->>$2 IS NOT NULL`, schema), item, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		if n, err := strconv.Atoi(p); err == nil {
			out[id] = n
		}
	}
	return out, rows.Err()
}

// ListMediaForItemChannel is the item's media in the order a channel gets.
func ListMediaForItemChannel(tenantID, item, channel string) ([]ProductMediaAsset, bool, error) {
	media, err := ListMediaForItem(tenantID, item)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(channel) == "" {
		return media, false, nil
	}
	pos, err := channelImagePositions(tenantID, item, channel)
	if err != nil {
		return nil, false, err
	}
	return OrderMediaForChannel(media, pos), len(pos) > 0, nil
}

// SetChannelImageOrder stores the order of an item's images for one channel;
// no ids clears it, so the channel goes back to today's order.
func SetChannelImageOrder(tenantID, userID, item, channel string, ids []string) (int, error) {
	bad := func(msg string) error {
		return &ValidationError{Code: channelOrderCode, SubFor: "Images", Message: msg}
	}
	item, channel = strings.TrimSpace(item), strings.TrimSpace(channel)
	if item == "" || channel == "" {
		return 0, bad("choose the item and the channel")
	}
	if _, _, err := fetchDocData(tenantID, "Channel", channel); err != nil {
		return 0, bad(fmt.Sprintf("channel %s was not found", channel))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND status = 'Active' AND deleted_at IS NULL FOR UPDATE`, schema), item)
	if err != nil {
		return 0, err
	}
	type row struct {
		id   string
		data map[string]interface{}
	}
	var all []row
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		m := map[string]interface{}{}
		_ = json.Unmarshal(raw, &m)
		all = append(all, row{id, m})
	}
	rows.Close()
	position := map[string]int{}
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if _, dup := position[id]; dup {
			return 0, bad("an image is listed twice")
		}
		position[id] = i
	}
	known := map[string]bool{}
	for _, r := range all {
		known[r.id] = strings.HasPrefix(strField(r.data, "file_type"), "image/")
	}
	for id := range position {
		if !known[id] {
			return 0, bad(fmt.Sprintf("%s is not an active image of %s", id, item))
		}
	}
	changed := 0
	for _, r := range all {
		cp, _ := r.data["channel_positions"].(map[string]interface{})
		if cp == nil {
			cp = map[string]interface{}{}
		}
		p, in := position[r.id]
		if in {
			cp[channel] = p
		} else {
			delete(cp, channel)
		}
		if len(cp) == 0 {
			delete(r.data, "channel_positions")
		} else {
			r.data["channel_positions"] = cp
		}
		body, _ := json.Marshal(r.data)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, updated_at = NOW(), updated_by = NULLIF($3, '')
			WHERE id = $1 AND doctype = 'ProductMedia' AND data IS DISTINCT FROM $2::jsonb`, schema), r.id, body, userID); err != nil {
			return 0, err
		}
		if in {
			changed++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "pim.media.channel_order", "Success", fmt.Sprintf("%s on %s: %d image(s) ordered", item, channel, changed))
	return changed, nil
}
