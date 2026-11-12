package engines

import "testing"

// Stage 146.4.6(e): a return with no store takes its sales channel's default
// return store; no default = today's behaviour.
func TestStage14646eDefaultReturnStorePerChannel(t *testing.T) {
	tenantID, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	fields, _ := GetDocTypeMeta(tenantID, "Channel")
	found := false
	for _, f := range fields {
		found = found || f.Fieldname == "default_return_location"
	}
	if !found {
		t.Fatalf("Channel needs a default_return_location field")
	}
	insert("CH-AMZ", "Channel", "Active", map[string]interface{}{"code": "AMZ", "name": "Amazon", "channel_type": "Marketplace", "enabled": "Yes", "default_return_location": "WH-RET"})
	insert("CH-WEB", "Channel", "Active", map[string]interface{}{"code": "WEB", "name": "Website", "channel_type": "Website", "enabled": "Yes"})
	insert("SO-E1", "SalesOrder", "Completed", map[string]interface{}{"code": "SO-E1", "channel": "AMZ"})
	insert("SO-E2", "SalesOrder", "Completed", map[string]interface{}{"code": "SO-E2", "channel": "Website"})
	insert("SO-E3", "SalesOrder", "Completed", map[string]interface{}{"code": "SO-E3", "channel": "Amazon"})
	insert("SO-E4", "SalesOrder", "Completed", map[string]interface{}{"code": "SO-E4"})
	for id, want := range map[string]string{"SO-E1": "WH-RET", "SO-E2": "", "SO-E3": "WH-RET", "SO-E4": "", "SO-NONE": ""} {
		if got := channelDefaultReturnStore(schema, id); got != want {
			t.Errorf("%s: want %q got %q", id, want, got)
		}
	}
}
