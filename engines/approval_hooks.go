package engines

import "sync"

// Stage 75.2: engines that act on an approval decision register here, instead
// of each adding its own line to DecideApproval (approval.go runs them once,
// after the decision is saved). A hook never fails the decision: it is called
// after the approval has committed, and reports its own failures.
type ApprovalDecisionHook func(tenantID, docID, decision, actorUserID string)

var (
	approvalHooksMu sync.RWMutex
	approvalHooks   = map[string][]ApprovalDecisionHook{}
	// dualControlDoctypes always need a second person: Stage 92's "the
	// submitter may approve their own documents" never applies to them.
	dualControlDoctypes = map[string]bool{}
)

// RegisterApprovalDecisionHook runs fn after every decision on doctype.
// dualControl makes the doctype one its requester can never decide.
func RegisterApprovalDecisionHook(doctype string, dualControl bool, fn ApprovalDecisionHook) {
	approvalHooksMu.Lock()
	defer approvalHooksMu.Unlock()
	approvalHooks[doctype] = append(approvalHooks[doctype], fn)
	if dualControl {
		dualControlDoctypes[doctype] = true
	}
}

func runApprovalDecisionHooks(tenantID, doctype, docID, decision, actorUserID string) {
	approvalHooksMu.RLock()
	hooks := append([]ApprovalDecisionHook(nil), approvalHooks[doctype]...)
	approvalHooksMu.RUnlock()
	for _, h := range hooks {
		h(tenantID, docID, decision, actorUserID)
	}
}

func isDualControlDoctype(doctype string) bool {
	approvalHooksMu.RLock()
	defer approvalHooksMu.RUnlock()
	return dualControlDoctypes[doctype]
}
