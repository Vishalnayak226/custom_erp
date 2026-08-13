// Command minttoken prints a scratch bearer token for local verification runs.
//
// It touches no database and is only useful against a server started with the
// same JWT_SECRET, so it cannot mint anything usable against a real deployment
// whose secret it does not know. Kept in the tree because every verification
// pass otherwise needs the same throwaway program written from scratch.
//
// The user id must belong to a real, Active user row: apiMiddleware re-reads
// live user state on every request (Stage 29.8), so a made-up id is rejected as
// an expired session no matter how well-formed the token is.
//
//	go run ./cmd/minttoken                            # admin / Super Admin / default
//	go run ./cmd/minttoken manager1 "Store Manager"   # any other user and role
//	go run ./cmd/minttoken admin "Super Admin" acme   # any other tenant
package main

import (
	"fmt"
	"os"

	"custom_erp/engines"
)

func main() {
	userID, role, tenant := "admin", "Super Admin", "default"
	if len(os.Args) > 1 {
		userID = os.Args[1]
	}
	if len(os.Args) > 2 {
		role = os.Args[2]
	}
	if len(os.Args) > 3 {
		tenant = os.Args[3]
	}
	fmt.Print(engines.SignToken(userID, userID, role, tenant, ""))
}
