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

// ApprovalSubmitCheck (Stage 130.16, 74.3.2) is a check a document must pass
// before it goes for approval - DEC-19's PO terms, required at approval and
// never on a draft.
type ApprovalSubmitCheck func(tenantID, docID string, data map[string]interface{}) error

var approvalSubmitChecks = map[string][]ApprovalSubmitCheck{}

// RegisterApprovalSubmitCheck runs fn before every submission of doctype.
func RegisterApprovalSubmitCheck(doctype string, fn ApprovalSubmitCheck) {
	approvalHooksMu.Lock()
	defer approvalHooksMu.Unlock()
	approvalSubmitChecks[doctype] = append(approvalSubmitChecks[doctype], fn)
}

func runApprovalSubmitChecks(tenantID, doctype, docID string, data map[string]interface{}) error {
	approvalHooksMu.RLock()
	checks := append([]ApprovalSubmitCheck(nil), approvalSubmitChecks[doctype]...)
	approvalHooksMu.RUnlock()
	for _, c := range checks {
		if err := c(tenantID, docID, data); err != nil {
			return err
		}
	}
	return nil
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
