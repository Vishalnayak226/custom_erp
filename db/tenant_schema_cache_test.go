package db

import "testing"

// 248.1: a client's schema name is answered from the cache on a repeat call,
// forgotten on ForgetTenantSchema, never cached when unknown, and read every
// time with the window at 0.
func TestGetTenantSchemaCachesAndForgets(t *testing.T) {
	InitDB(ConnStringFromEnv())
	const id = "t2481-schema-cache"
	setSchema := func(q, schema string) {
		t.Helper()
		if _, err := DB.Exec(q, id, schema); err != nil {
			t.Fatal(err)
		}
	}
	const insert = `INSERT INTO public.tenants (tenant_id, name, schema_name) VALUES ($1, $1, $2)`
	const update = `UPDATE public.tenants SET schema_name = $2 WHERE tenant_id = $1`
	remove := func() {
		DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, id)
		ForgetTenantSchema(id)
	}
	remove()
	t.Cleanup(remove)
	t.Setenv("AUTH_STATE_CACHE_SECONDS", "30")
	want := func(step, schema string) {
		t.Helper()
		got, err := GetTenantSchema(id)
		if err != nil || got != schema {
			t.Fatalf("%s: GetTenantSchema = %q, %v; want %q", step, got, err, schema)
		}
	}

	setSchema(insert, "tenant_t2481a")
	want("first call", "tenant_t2481a")
	// The row changes behind the cache: a repeat call still answers the cached
	// name, so it made no query.
	setSchema(update, "tenant_t2481b")
	want("repeat call", "tenant_t2481a")
	if !TenantSchemaCached(id) {
		t.Fatal("the client is not reported as cached")
	}
	ForgetTenantSchema(id)
	want("after forget", "tenant_t2481b")

	// A removed client stops resolving.
	remove()
	if TenantSchemaCached(id) {
		t.Fatal("a removed client is still cached")
	}
	if got, err := GetTenantSchema(id); err != ErrUnknownTenant {
		t.Fatalf("removed client: GetTenantSchema = %q, %v; want ErrUnknownTenant", got, err)
	}
	// The unknown answer was not cached: registering the id is seen at once.
	setSchema(insert, "tenant_t2481c")
	want("registered after an unknown answer", "tenant_t2481c")

	// With the window at 0 every call reads the row.
	t.Setenv("AUTH_STATE_CACHE_SECONDS", "0")
	setSchema(update, "tenant_t2481d")
	want("cache off", "tenant_t2481d")
}
