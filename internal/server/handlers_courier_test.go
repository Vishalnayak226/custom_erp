package server

import "testing"

func TestCourierWebhookExposureIsProviderScoped(t *testing.T) {
	for _, path := range []string{
		"/api/v1/integration/courier/delhivery/tracking",
		"/api/v1/integration/courier/shiprocket/tracking",
	} {
		if !publicRoutes[path] {
			t.Fatalf("courier webhook %s requires a human bearer token", path)
		}
		category, limit := rateLimitCategory(path, "POST")
		if category != "webhook" || limit != 30 {
			t.Fatalf("%s rate category=%s/%d", path, category, limit)
		}
	}
	if publicRoutes["/api/v1/integration/courier/arbitrary/tracking"] {
		t.Fatal("unknown courier provider was accidentally made public")
	}
}
