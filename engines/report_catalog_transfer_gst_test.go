package engines

import "testing"

// An odd-paisa till bill: the GST reports split its tax the way the bill
// does, so CGST + SGST on the report rows equals the bill's tax to the paisa
// (ask 191: journeylab E8 found 11.90 + 11.90 against a bill of 23.81).
func TestGSTReportTillLineSplitsTaxLikeTheBill(t *testing.T) {
	r := &catRow{N: 1,
		H: map[string]interface{}{"gst_breakdown": map[string]interface{}{"cgst": 11.91, "sgst": 11.9, "igst": 0.0, "total_tax": 23.81}},
		L: map[string]interface{}{"sku": "X", "qty": 1.0, "sale_price": 500.0, "net_total": 500.0, "gst_rate": 5.0}}
	g := gstLine(r)
	if round2(g.cgst) != 11.91 || round2(g.sgst) != 11.9 {
		t.Fatalf("till line split %.4f + %.4f, want 11.91 + 11.90 as on the bill", g.cgst, g.sgst)
	}
	if round2(round2(g.cgst)+round2(g.sgst)) != round2(g.tax()) || round2(g.tax()) != 23.81 {
		t.Fatalf("rounded halves %.2f + %.2f do not make the line tax %.2f (bill 23.81)", round2(g.cgst), round2(g.sgst), g.tax())
	}
	// Interstate: the whole tax is IGST.
	r.H["gst_breakdown"] = map[string]interface{}{"igst": 23.81, "total_tax": 23.81}
	if g := gstLine(r); g.cgst != 0 || g.sgst != 0 || round2(g.igst) != 23.81 {
		t.Fatalf("interstate line: cgst %.2f sgst %.2f igst %.2f, want 0 / 0 / 23.81", g.cgst, g.sgst, g.igst)
	}
}
