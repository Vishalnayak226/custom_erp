// Command server is the ERP's actual entrypoint. All request handling,
// middleware, and route registration live in internal/server - this file is
// deliberately just a launcher (Stage 19 folder restructuring, 2026-07-19).
package main

import "custom_erp/internal/server"

func main() {
	server.Run()
}
