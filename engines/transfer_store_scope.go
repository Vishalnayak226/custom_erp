package engines

import (
	"errors"
	"fmt"
	"strings"
)

// Item 193.3: a store-limited user moves stock only at their own stores.
// Creating a transfer needs its source or its destination store, dispatch
// needs the source, receipt the destination. One check, called from the
// engine functions every path goes through (the screen, the store app, the
// generic record API, imports); the report cut (report_store_scope.go) uses
// the same StoreAccess test, so what a user may move is what they may see.
//
// Unchanged: an empty user id (an internal run), a user the system does not
// know (not a signed-in person), Super Admin / full-access roles, a role
// whose template grants global location scope, and "All stores" users.
// Approval keeps the maker-checker engine's own rules.

// transferStep is the part of a transfer's life being attempted.
type transferStep string

const (
	transferStepCreate   transferStep = "create"
	transferStepDispatch transferStep = "dispatch"
	transferStepReceive  transferStep = "receive"
)

// CheckTransferStoreAccess refuses a transfer step at stores the user does
// not work at. from and to are the transfer's from_warehouse / to_warehouse.
func CheckTransferStoreAccess(tenantID, userID, step string, from, to string) error {
	return checkTransferStoreAccess(tenantID, userID, transferStep(step), from, to)
}

func checkTransferStoreAccess(tenantID, userID string, step transferStep, from, to string) error {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	live, err := ResolveLiveUserState(tenantID, userID)
	if err != nil {
		if errors.Is(err, errUserNotActive) {
			return nil // not a signed-in person (a system actor)
		}
		return err // fail closed
	}
	return transferStoreDecision(live, step, from, to)
}

// transferStoreDecision is the rule itself, on an already-resolved user.
func transferStoreDecision(live LiveUserState, step transferStep, from, to string) error {
	if HasFullAccess(live.Role) || roleTemplateGrantsGlobalScope(live.Role, ScopeLocation) || live.Access.All {
		return nil
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	var need, what string
	switch step {
	case transferStepDispatch:
		if live.Access.Allows(from) {
			return nil
		}
		need, what = from, "dispatch a transfer from"
	case transferStepReceive:
		if live.Access.Allows(to) {
			return nil
		}
		need, what = to, "receive a transfer at"
	default:
		if from == "" && to == "" {
			return nil // nothing named yet; mandatory-field rules judge the save
		}
		if live.Access.Allows(from) || live.Access.Allows(to) {
			return nil
		}
		need, what = firstNonEmpty(from, to), "create a transfer between"
		if from != "" && to != "" {
			need = from + " and " + to
		}
	}
	return &ValidationError{Code: "STORE-0855", Message: fmt.Sprintf("You may not %s %s - it is not one of your stores.", what, need)}
}
