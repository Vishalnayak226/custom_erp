package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 55.17 - vouchers and campaigns in words, for the Loyalty screen's
// "Vouchers & campaigns" tab. Read-only: both are still set up on their own
// records (Setup -> Voucher / Campaign). What this adds is the preview the
// usability plan asks for - who a voucher is for, where and when it works,
// whether it can be used right now and why not, and what it has given away;
// who a campaign would pick today and what it has brought in.
//
// Each answer comes from the rule that enforces it, never a copy: a voucher's
// state is checkVoucherUsable's own verdict (the check the till and the online
// redeem run), a campaign's picks are the worker's own selection
// (birthdayCampaignMatches / lapsedCampaignMatches), its results are
// GetCampaignROIReport's.

// VoucherOverview is one voucher in words.
type VoucherOverview struct {
	Code          string  `json:"code"`
	Gives         string  `json:"gives"`
	For           string  `json:"for"`
	Where         string  `json:"where"`
	ValidFrom     string  `json:"valid_from,omitempty"`
	ExpiryDate    string  `json:"expiry_date,omitempty"`
	MaxUses       int     `json:"max_uses"`
	UsedCount     int     `json:"used_count"`
	Usable        bool    `json:"usable"`
	State         string  `json:"state"`
	DiscountGiven float64 `json:"discount_given"`
}

// ListVoucherOverview lists the tenant's vouchers, newest first.
func ListVoucherOverview(tenantID string) ([]VoucherOverview, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// What each voucher took off paid bills: the quote records an applied
	// voucher as offer "VOUCHER:<code>" (pos_quote.go applyTillVoucher).
	given := map[string]float64{}
	gRows, err := db.DB.Query(fmt.Sprintf(`SELECT o->>'offer_id', COALESCE(SUM((o->>'discount')::numeric), 0)
		FROM %s.documents c, jsonb_array_elements(CASE WHEN jsonb_typeof(c.data->'applied_offers') = 'array' THEN c.data->'applied_offers' ELSE '[]'::jsonb END) o
		WHERE c.doctype = 'POSCart' AND c.status = 'Paid' AND o->>'offer_type' = $1 GROUP BY 1`, schema), OfferTypeVoucher)
	if err != nil {
		return nil, err
	}
	for gRows.Next() {
		var id string
		var amount float64
		if gRows.Scan(&id, &amount) == nil {
			given[strings.ToUpper(strings.TrimPrefix(id, "VOUCHER:"))] = round2(amount)
		}
	}
	gRows.Close()

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT v.data, COALESCE(c.data->>'name', '') FROM %s.documents v
		LEFT JOIN %s.documents c ON c.doctype = 'Customer' AND c.id = v.data->>'customer_id'
		WHERE v.doctype = 'Voucher' AND v.deleted_at IS NULL ORDER BY v.created_at DESC LIMIT 500`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VoucherOverview{}
	for rows.Next() {
		var dataStr, customerName string
		if err := rows.Scan(&dataStr, &customerName); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		v := voucherFromData(data)
		o := VoucherOverview{Code: v.Code, ValidFrom: v.ValidFrom, ExpiryDate: v.ExpiryDate, MaxUses: v.MaxUses, UsedCount: v.UsedCount,
			DiscountGiven: given[strings.ToUpper(v.Code)], For: "Any customer", Where: "At the till and online"}
		if v.DiscountType == "Flat" {
			o.Gives = fmt.Sprintf("%d off the bill", v.DiscountValue)
		} else {
			o.Gives = fmt.Sprintf("%d%% off the bill", v.DiscountValue)
		}
		if v.CustomerID != "" {
			o.For = customerName
			if o.For == "" {
				o.For = v.CustomerID
			}
		}
		switch {
		case strings.EqualFold(v.Channel, VoucherChannelPOS):
			o.Where = "At the till only"
		case strings.EqualFold(v.Channel, VoucherChannelOnline):
			o.Where = "Online only"
		}
		// The same verdict the till gives, for the customer it is meant for.
		if err := checkVoucherUsable(v, v.CustomerID, ""); err != nil {
			o.State = "Not usable: " + strings.TrimPrefix(err.Error(), fmt.Sprintf("voucher %q ", v.Code))
		} else {
			o.Usable, o.State = true, "Usable now"
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CampaignOverview is one campaign in words.
type CampaignOverview struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Trigger        string   `json:"trigger"`
	Status         string   `json:"status"`
	MessagePreview string   `json:"message_preview"`
	Channel        string   `json:"channel"`
	Note           string   `json:"note,omitempty"`
	PicksToday     int      `json:"picks_today"`
	NoConsent      int      `json:"skipped_no_consent"`
	SentTo         int      `json:"sent_to"`
	SalesSince     float64  `json:"sales_since"`
	Cost           float64  `json:"cost"`
	ROI            *float64 `json:"roi,omitempty"`
}

// ListCampaignOverview lists the tenant's campaigns with today's picks and
// their results so far.
func ListCampaignOverview(tenantID string) ([]CampaignOverview, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	roi, err := GetCampaignROIReport(tenantID)
	if err != nil {
		return nil, err
	}
	results := map[string]CampaignROIRow{}
	for _, r := range roi {
		results[r.CampaignID] = r
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, data FROM %s.documents
		WHERE doctype = 'Campaign' AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	type campaignDoc struct{ id, status, data string }
	var docs []campaignDoc
	for rows.Next() {
		var d campaignDoc
		if rows.Scan(&d.id, &d.status, &d.data) == nil {
			docs = append(docs, d)
		}
	}
	rows.Close()

	out := []CampaignOverview{}
	for _, d := range docs {
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(d.data), &data)
		template, _ := data["message_template"].(string)
		o := CampaignOverview{ID: d.id, Name: strField(data, "name"), Status: d.status, Channel: strings.TrimSpace(strField(data, "channel"))}
		if consentField(o.Channel) == "" {
			o.Note = "Choose the channel it is sent by (SMS or WhatsApp) - nothing is sent until then"
		}
		// Stage 55.17a: picks are the customers who agreed to offers on the
		// campaign's channel; the rest of the trigger's matches are skipped.
		switch trigger, _ := data["trigger_type"].(string); trigger {
		case "Birthday":
			o.Trigger = "On the customer's birthday"
			o.MessagePreview = renderTemplate(template, map[string]string{"customer_name": "Asha"})
			all, errAll := birthdayCampaignMatches(schema, o.Channel, false)
			picks, errPicks := birthdayCampaignMatches(schema, o.Channel, true)
			if errAll == nil && errPicks == nil {
				o.PicksToday, o.NoConsent = len(picks), len(all)-len(picks)
			}
		case "Lapsed Customer":
			days := campaignLapsedDays(schema, data)
			o.Trigger = fmt.Sprintf("No purchase in %d days", days)
			o.MessagePreview = renderTemplate(template, map[string]string{})
			all, errAll := lapsedCampaignMatches(schema, days, o.Channel, false)
			picks, errPicks := lapsedCampaignMatches(schema, days, o.Channel, true)
			if errAll == nil && errPicks == nil {
				o.PicksToday, o.NoConsent = len(picks), len(all)-len(picks)
			}
		default:
			o.Trigger = "Unknown trigger " + trigger + " - the worker skips it"
		}
		if r, ok := results[d.id]; ok {
			o.SentTo, o.SalesSince, o.Cost, o.ROI = r.CustomersTargeted, round2(r.AttributedRevenue), r.Cost, r.ROI
		}
		out = append(out, o)
	}
	return out, nil
}
