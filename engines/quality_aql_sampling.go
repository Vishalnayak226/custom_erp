package engines

import (
	"fmt"
	"strconv"
	"strings"
)

// 170.15 - AQL sampling. ISO 2859-1 / ANSI Z1.4: general inspection level II,
// single sampling, normal inspection. A lot's size gives a code letter, the
// letter and the AQL give a sample size and the accept / reject numbers.
//
// The table is held as the standard builds it: sample sizes by code letter, and
// for each AQL the plan ladder (Ac/Re 0/1, 1/2, 2/3, 3/4, 5/6, 7/8, 10/11,
// 14/15, 21/22) laid along the letters. A letter that falls where the standard
// prints an arrow uses the nearest plan the arrow points to (so its sample size
// is that plan's, not its own letter's). When the sample would be the whole lot
// or more, the whole lot is inspected.

var aqlLevels = []string{"0.065", "0.10", "0.15", "0.25", "0.40", "0.65", "1.0", "1.5", "2.5", "4.0", "6.5"}

// aqlLotBands: upper lot size of each code letter A..Q (level II).
var aqlLotBands = []struct {
	Letter string
	Upto   int
	Sample int
}{
	{"A", 8, 2}, {"B", 15, 3}, {"C", 25, 5}, {"D", 50, 8}, {"E", 90, 13}, {"F", 150, 20}, {"G", 280, 32},
	{"H", 500, 50}, {"J", 1200, 80}, {"K", 3200, 125}, {"L", 10000, 200}, {"M", 35000, 315},
	{"N", 150000, 500}, {"P", 500000, 800}, {"Q", 1 << 30, 1250},
}

// aqlAccept is the accept number at ladder step t (letter index + AQL index - 10).
var aqlAccept = map[int]int{0: 0, 3: 1, 4: 2, 5: 3, 6: 5, 7: 7, 8: 10, 9: 14, 10: 21}

// AQLPlan is the sampling plan for one lot.
type AQLPlan struct {
	AQL            string `json:"aql"`
	LotSize        int    `json:"lot_size"`
	CodeLetter     string `json:"code_letter"`
	SampleSize     int    `json:"sample_size"`
	AcceptNo       int    `json:"accept_no"`
	RejectNo       int    `json:"reject_no"`
	FullInspection bool   `json:"full_inspection"`
}

// AQLLevels lists the AQLs the table covers.
func AQLLevels() []string { return append([]string(nil), aqlLevels...) }

// NormalizeAQL maps "1", "1.0", "1.00" or "0.1" to the table's spelling, or "".
func NormalizeAQL(v string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return ""
	}
	for _, l := range aqlLevels {
		if x, _ := strconv.ParseFloat(l, 64); x == f {
			return l
		}
	}
	return ""
}

// AQLSamplePlan returns the plan for a lot of lotSize at the given AQL.
func AQLSamplePlan(lotSize int, aql string) (AQLPlan, error) {
	level := NormalizeAQL(aql)
	if level == "" {
		return AQLPlan{}, &ValidationError{Code: "QUALIT-1760", Message: "pick an AQL from the table: " + strings.Join(aqlLevels, ", ")}
	}
	if lotSize < 1 {
		return AQLPlan{}, &ValidationError{Code: "QUALIT-1761", Message: "the lot size must be at least 1"}
	}
	j := 0
	for i, l := range aqlLevels {
		if l == level {
			j = i
		}
	}
	i := 0
	for k, b := range aqlLotBands {
		if lotSize <= b.Upto {
			i = k
			break
		}
	}
	// Ladder step; the arrow cells (before 0 and between 0 and 3) and the cells
	// past the last plan use the 0/1 plan or the last plan the arrow points to.
	t := i + j - 10
	switch {
	case t < 0 || t == 1 || t == 2:
		i, t = 10-j, 0
	case t > 10:
		i, t = 20-j, 10
	}
	ac := aqlAccept[t]
	p := AQLPlan{AQL: level, LotSize: lotSize, CodeLetter: aqlLotBands[i].Letter, SampleSize: aqlLotBands[i].Sample, AcceptNo: ac, RejectNo: ac + 1}
	// The code letter is the lot's own; the sample is from the plan used.
	for _, b := range aqlLotBands {
		if lotSize <= b.Upto {
			p.CodeLetter = b.Letter
			break
		}
	}
	if p.SampleSize >= lotSize {
		p.SampleSize, p.FullInspection = lotSize, true
	}
	return p, nil
}

// Decide is the lot result from the defects found in the sample.
func (p AQLPlan) Decide(defects float64) string {
	if defects <= float64(p.AcceptNo) {
		return "Accept"
	}
	return "Reject"
}

// inspectionPlanAQL is the AQL an inspection plan names, "" when it names none.
func inspectionPlanAQL(tenantID, planID string) string {
	if strings.TrimSpace(planID) == "" {
		return ""
	}
	plan, _, err := fetchDocData(tenantID, "InspectionPlan", planID)
	if err != nil || plan == nil {
		return ""
	}
	return NormalizeAQL(strField(plan, "aql"))
}

// applyAQLToInspection fills the sample plan and result onto an inspection's
// data when its plan names an AQL. Returns the lot decision ("" when no AQL).
// An inspection whose plan has no AQL is untouched.
func applyAQLToInspection(tenantID string, in InspectionInput, lotQty float64, data map[string]interface{}) (string, error) {
	aql := inspectionPlanAQL(tenantID, in.InspectionPlan)
	if aql == "" {
		return "", nil
	}
	if lotQty <= 0 || lotQty != float64(int(lotQty)) {
		return "", &ValidationError{Code: "QUALIT-1761", Message: "enter the lot size as a whole number of units"}
	}
	plan, err := AQLSamplePlan(int(lotQty), aql)
	if err != nil {
		return "", err
	}
	data["aql"], data["aql_code_letter"], data["sample_size"] = plan.AQL, plan.CodeLetter, plan.SampleSize
	data["accept_no"], data["reject_no"], data["lot_size"] = plan.AcceptNo, plan.RejectNo, plan.LotSize
	if in.Defects == nil || *in.Defects < 0 {
		return "", &ValidationError{Code: "QUALIT-1762", Message: fmt.Sprintf("enter the defects found in the %d sampled units", plan.SampleSize)}
	}
	data["defects_found"] = *in.Defects
	res := plan.Decide(*in.Defects)
	data["aql_result"] = res
	return res, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "aql-sample-plan", Label: "AQL Sample Plan", Category: "Quality", ModuleKey: "quality", StoreKey: "-",
		Columns: []ReportColumn{
			{Key: "lot_size", Label: "Lot size"}, {Key: "aql", Label: "AQL"}, {Key: "code_letter", Label: "Code letter"},
			{Key: "sample_size", Label: "Sample"}, {Key: "accept_no", Label: "Accept at"}, {Key: "reject_no", Label: "Reject at"},
			{Key: "full_inspection", Label: "Whole lot"},
		},
		Params: []ReportParam{
			{Key: "lot_size", Label: "Lot size", Type: "text", Required: true},
			{Key: "aql", Label: "AQL", Type: "select", Options: strings.Join(aqlLevels, ",")},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			n, err := strconv.Atoi(strings.TrimSpace(params["lot_size"]))
			if err != nil {
				return nil, &ValidationError{Code: "QUALIT-1761", Message: "the lot size must be a whole number"}
			}
			aql := params["aql"]
			if strings.TrimSpace(aql) == "" {
				aql = "2.5"
			}
			p, err := AQLSamplePlan(n, aql)
			if err != nil {
				return nil, err
			}
			whole := "No"
			if p.FullInspection {
				whole = "Yes"
			}
			return []map[string]interface{}{{"lot_size": p.LotSize, "aql": p.AQL, "code_letter": p.CodeLetter,
				"sample_size": p.SampleSize, "accept_no": p.AcceptNo, "reject_no": p.RejectNo, "full_inspection": whole}}, nil
		},
	})
}
