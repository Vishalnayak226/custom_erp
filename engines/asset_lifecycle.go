package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"time"
)

// Stage 55.16: one asset's readable lifecycle - where it came from, its
// straight-line schedule, the GL lines actually posted for it with who
// posted them, and its transfers from the audit trail.
//
// Batch D (the user's "post depreciation monthly"): the schedule is the
// monthly plan engines/asset_depreciation.go posts, grouped by year of life,
// each year showing what is posted and what is to come. Gaps name only a real
// lag - months that have ended but are not posted yet, and why - plus the
// old-style entry of an asset disposed before depreciation was posted.

type AssetScheduleLine struct {
	Year         int     `json:"year"`
	From         string  `json:"from"`
	To           string  `json:"to"`
	Opening      float64 `json:"opening"`
	Depreciation float64 `json:"depreciation"`
	Closing      float64 `json:"closing"`
	Posted       float64 `json:"posted"`
	ToCome       float64 `json:"to_come"`
	Months       int     `json:"months"`
	MonthsPosted int     `json:"months_posted"`
	Current      bool    `json:"current"`
}

// AssetDepreciationStatus is the asset's monthly plan against the books.
type AssetDepreciationStatus struct {
	MonthlyAmount   float64                  `json:"monthly_amount"`
	TotalMonths     int                      `json:"total_months"`
	MonthsDue       int                      `json:"months_due"`
	MonthsPosted    int                      `json:"months_posted"`
	Posted          float64                  `json:"posted"`
	Due             float64                  `json:"due"`
	MonthsNotPosted []string                 `json:"months_not_posted"`
	Postings        []map[string]interface{} `json:"postings"`
}

type AssetLifecycle struct {
	Asset        AssetRegisterEntry       `json:"asset"`
	Acquisition  map[string]interface{}   `json:"acquisition"`
	Annual       int                      `json:"annual_depreciation"`
	Schedule     []AssetScheduleLine      `json:"schedule"`
	Depreciation AssetDepreciationStatus  `json:"depreciation"`
	GL           []map[string]interface{} `json:"gl_postings"`
	Transfers    []map[string]interface{} `json:"transfers"`
	Gaps         []string                 `json:"gaps"`
}

// GetAssetLifecycle reads one asset's lifecycle.
func GetAssetLifecycle(tenantID, assetID string) (*AssetLifecycle, error) {
	register, err := GetAssetRegister(tenantID)
	if err != nil {
		return nil, err
	}
	var entry *AssetRegisterEntry
	for i := range register {
		if register[i].ID == assetID {
			entry = &register[i]
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("asset not found")
	}
	data, _, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	lc := &AssetLifecycle{Asset: *entry, Acquisition: map[string]interface{}{
		"acquisition_date": data["acquisition_date"], "source_grn": entry.SourceGRN, "item_code": entry.ItemCode,
		"source":        map[bool]string{true: "Goods receipt " + entry.SourceGRN, false: "Entered by hand"}[entry.SourceGRN != ""],
		"disposal_type": data["disposal_type"], "disposal_date": data["disposal_date"],
		"disposal_proceeds": data["disposal_proceeds"], "disposal_received_into": data["disposal_received_into"],
		"disposal_book_value": data["disposal_book_value"], "disposal_gain_loss": data["disposal_gain_loss"],
	}}

	if entry.UsefulLifeYears > 0 && entry.Cost > 0 {
		lc.Annual = int(entry.Cost) / entry.UsefulLifeYears
	}
	posted := map[string]assetDepPosting{}
	if entry.Status != "Draft" {
		if posted, err = loadAssetDepreciationPostings(db.DB, schema, assetID); err != nil {
			return nil, err
		}
	}
	capDate := entry.CapitalisationDate
	if capDate == "" {
		capDate = time.Now().Format("2006-01-02") // a Draft: the preview capitalising today would give
	}
	st := &lc.Depreciation
	st.MonthsNotPosted, st.Postings = []string{}, []map[string]interface{}{}
	plan, planned := assetDepreciationPlan(data, capDate)
	if planned {
		today := time.Now().Format("2006-01-02")
		if entry.Status == "Capitalised" {
			st.MonthsDue = plan.monthsDue(time.Now())
		}
		st.MonthlyAmount, st.TotalMonths = PaiseToRupees(plan.amount(1)), plan.months
		opening := plan.costPaise
		for y := 1; (y-1)*12 < plan.months; y++ {
			first, last := (y-1)*12+1, y*12
			if last > plan.months {
				last = plan.months
			}
			var dep, done int64
			monthsPosted := 0
			for i := first; i <= last; i++ {
				dep += plan.amount(i)
				if p, ok := posted[plan.monthKey(i)]; ok {
					done += p.amount
					monthsPosted++
				}
			}
			toCome := dep - done
			if entry.Status == "Disposed" {
				toCome = 0 // nothing more posts once the asset is gone
			}
			from, to := plan.monthStart(first).Format("2006-01-02"), plan.monthEnd(last).Format("2006-01-02")
			lc.Schedule = append(lc.Schedule, AssetScheduleLine{
				Year: y, From: from, To: to, Opening: PaiseToRupees(opening), Depreciation: PaiseToRupees(dep),
				Closing: PaiseToRupees(opening - dep), Posted: PaiseToRupees(done), ToCome: PaiseToRupees(toCome),
				Months: last - first + 1, MonthsPosted: monthsPosted,
				Current: entry.Status == "Capitalised" && from <= today && today <= to,
			})
			opening -= dep
		}
		var duePaise int64
		for i := 1; i <= st.MonthsDue; i++ {
			duePaise += plan.amount(i)
			if _, ok := posted[plan.monthKey(i)]; !ok {
				st.MonthsNotPosted = append(st.MonthsNotPosted, plan.monthKey(i))
			}
		}
		st.Due = PaiseToRupees(duePaise)
	}
	months := make([]string, 0, len(posted))
	for month := range posted {
		months = append(months, month)
	}
	sort.Strings(months)
	var postedPaise int64
	for _, month := range months {
		p := posted[month]
		postedPaise += p.amount
		by := p.postedBy
		if by == "" {
			by = glBeforeAttribution
		}
		st.Postings = append(st.Postings, map[string]interface{}{"month": month, "amount": PaiseToRupees(p.amount), "dated": p.postedAt, "posted_by": by})
	}
	st.MonthsPosted, st.Posted = len(months), PaiseToRupees(postedPaise)

	// Depreciation months are listed under Depreciation, one row a month; the
	// GL table keeps capitalisation and disposal.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT to_char(created_at, 'YYYY-MM-DD HH24:MI'), document_type, account_code, debit, credit, COALESCE(posted_by, '')
		FROM %s.gl_postings WHERE document_id = $1 AND document_type IN ('Asset', 'Asset-Disposal')
		  AND COALESCE(idempotency_key, '') NOT LIKE '%%:DEP:%%'
		ORDER BY created_at, posting_id`, schema), assetID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var at, docType, account, by string
		var debit, credit int64
		if err := rows.Scan(&at, &docType, &account, &debit, &credit, &by); err != nil {
			rows.Close()
			return nil, err
		}
		if by == "" {
			by = glBeforeAttribution
		}
		lc.GL = append(lc.GL, map[string]interface{}{
			"posted_at": at, "event": map[string]string{"Asset": "Capitalised", "Asset-Disposal": "Disposed"}[docType],
			"account_code": account, "debit": PaiseToRupees(debit), "credit": PaiseToRupees(credit), "posted_by": by,
		})
	}
	rows.Close()

	tr, err := db.DB.Query(fmt.Sprintf(`
		SELECT to_char(created_at, 'YYYY-MM-DD HH24:MI'), COALESCE(user_id, ''), COALESCE(details, '')
		FROM %s.audit_logs WHERE action = 'ASSET_TRANSFER' AND details LIKE $1 ORDER BY created_at`, schema), "Asset "+assetID+" transferred%")
	if err == nil {
		for tr.Next() {
			var at, by, details string
			if tr.Scan(&at, &by, &details) == nil {
				lc.Transfers = append(lc.Transfers, map[string]interface{}{"at": at, "by": by, "detail": details})
			}
		}
		tr.Close()
	}

	if entry.Status == "Capitalised" && !planned {
		lc.Gaps = append(lc.Gaps, "No useful life or capitalisation date is recorded, so no depreciation is worked out or posted for this asset.")
	}
	if entry.Status == "Capitalised" && len(st.MonthsNotPosted) > 0 {
		closed, err := closedDepreciationMonths(schema, assetID, st.MonthsNotPosted)
		if err != nil {
			return nil, err
		}
		var pending []string
		var pendingPaise int64
		for i := 1; i <= st.MonthsDue; i++ {
			month := plan.monthKey(i)
			if _, done := posted[month]; done {
				continue
			}
			if reason, isClosed := closed[month]; isClosed {
				lc.Gaps = append(lc.Gaps, fmt.Sprintf("Depreciation for %s is not posted: %s.", month, reason))
				continue
			}
			pending = append(pending, month)
			pendingPaise += plan.amount(i)
		}
		if len(pending) > 0 {
			span := pending[0]
			if len(pending) > 1 {
				span = pending[0] + " to " + pending[len(pending)-1]
			}
			lc.Gaps = append(lc.Gaps, fmt.Sprintf("%d month(s) of depreciation (%.2f, %s) have ended but are not posted yet - the monthly run posts them, or an Accountant can choose Run depreciation.",
				len(pending), PaiseToRupees(pendingPaise), span))
		}
	}
	if entry.Status == "Disposed" && data["disposal_proceeds"] == nil {
		// Disposed before Batch D: that entry stands as it was posted.
		lc.Gaps = append(lc.Gaps, "Disposed before depreciation was posted: the disposal wrote off only the remaining net block, the depreciated part stayed in 1400, and no sale proceeds are recorded.")
	}
	if lc.Schedule == nil {
		lc.Schedule = []AssetScheduleLine{}
	}
	if lc.GL == nil {
		lc.GL = []map[string]interface{}{}
	}
	if lc.Transfers == nil {
		lc.Transfers = []map[string]interface{}{}
	}
	if lc.Gaps == nil {
		lc.Gaps = []string{}
	}
	return lc, nil
}
