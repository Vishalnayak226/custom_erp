package engines

import (
	"fmt"

	"custom_erp/db"
)

// Stage 148.30: R3095, the incentive ledger by salesman and period.
func init() {
	RegisterReport(ReportDefinition{
		ID: "sales-incentives", Label: "Sales incentives", Category: "Sales", ModuleKey: ModuleSalesIncentives,
		Columns: []ReportColumn{
			{Key: "salesman_id", Label: "Salesman"}, {Key: "accrued", Label: "Accrued"}, {Key: "clawed_back", Label: "Clawed back"},
			{Key: "paid", Label: "Paid"}, {Key: "balance", Label: "Balance"},
		},
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			if err := RequireSalesIncentives(tenantID); err != nil {
				return nil, err
			}
			schema, err := db.GetTenantSchema(tenantID)
			if err != nil {
				return nil, err
			}
			rows, err := db.DB.Query(fmt.Sprintf(`SELECT salesman_id,
					COALESCE(SUM(amount_paise) FILTER (WHERE kind = 'Accrual'),0),
					COALESCE(-SUM(amount_paise) FILTER (WHERE kind = 'Clawback'),0),
					COALESCE(SUM(amount_paise) FILTER (WHERE paid_payslip <> ''),0),
					COALESCE(SUM(amount_paise) FILTER (WHERE paid_payslip = ''),0)
				FROM %s.sales_incentive_entries
				WHERE ($1 = '' OR entry_date >= $1::date) AND ($2 = '' OR entry_date <= $2::date)
				GROUP BY salesman_id ORDER BY salesman_id`, schema), params["from"], params["to"])
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			var out []map[string]interface{}
			for rows.Next() {
				var sm string
				var a, c, p, b int64
				if err := rows.Scan(&sm, &a, &c, &p, &b); err != nil {
					return nil, err
				}
				out = append(out, map[string]interface{}{"salesman_id": sm, "accrued": PaiseToRupees(a), "clawed_back": PaiseToRupees(c), "paid": PaiseToRupees(p), "balance": PaiseToRupees(b)})
			}
			return out, nil
		},
	})
}
