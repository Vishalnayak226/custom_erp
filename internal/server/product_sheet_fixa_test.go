package server

import "testing"

// Review fix (S4): a product sheet of a few thousand rows is bigger than the
// 2 MiB default, so both routes carry the import-sized body cap.
func TestStage14654SheetRoutesTakeImportSizedBody(t *testing.T) {
	for _, p := range []string{"/api/v1/pim/product-sheet/preview", "/api/v1/pim/product-sheet/import"} {
		if got := envelopePolicyFor(p).maxBody; got != 16<<20 {
			t.Fatalf("%s maxBody = %d", p, got)
		}
	}
}
