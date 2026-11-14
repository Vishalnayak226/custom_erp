package engines

import (
	"errors"
	"testing"
)

// A stewardship entry that lists markets covers those markets only, as one that
// lists channels or languages covers only those. A market outside the list is
// free only on its own row: declaring another market while aiming at the shared
// row must not step round the rule.
func TestStewardshipMarketNarrowsSurvivorship(t *testing.T) {
	tenantID, _, insert := stewardFixture(t)
	entry := stewardSet("COLOR", "Channel", "AMZ", stewardSurvivorWins)
	delete(entry, "channels")
	entry["markets"] = "IN"
	stewardChange(t, tenantID, insert, "STC-M1", entry)

	write := func(id, market, value string) error {
		p := map[string]interface{}{"code": id, "id": id, "item": "ITM-1", "attribute": "COLOR", "value": value, "status": "Active", "source": "Import"}
		if market != "" {
			p["market"] = market
		}
		return ValidateDocument(tenantID, "ProductAttributeValue", p)
	}
	refused := func(label string, err error) {
		t.Helper()
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Code != "PIM-9151" {
			t.Errorf("%s: want PIM-9151, got %v", label, err)
		}
	}
	refused("no market, shared row", write("ITM-1::COLOR", "", "Blue"))
	refused("covered market IN", write(attributeValueID("ITM-1", "COLOR", "", "", "IN"), "IN", "Blue"))
	refused("other market aimed at the shared row", write("ITM-1::COLOR", "UK", "Blue"))
	refused("other market aimed at another market's row", write(attributeValueID("ITM-1", "COLOR", "", "", "IN"), "UK", "Blue"))
	if err := write(attributeValueID("ITM-1", "COLOR", "", "", "UK"), "UK", "Blue"); err != nil {
		t.Errorf("a market the entry does not list, on its own row, should save: %v", err)
	}
	if got := attributeValueID("ITM-1", "COLOR", "", ""); got != "ITM-1::COLOR" {
		t.Errorf("an id without a market changed: %s", got)
	}
}
