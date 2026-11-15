package engines

import (
	"database/sql"
	"errors"
)

// Ask 176 item 176.5: service knowledge base.
//
// A ServiceArticle is a how-to or a known fix, filed by category. The record
// list's search already reads every field, so the article text is
// searchable as it is. A service ticket names the article that solved it
// (kb_article), and only a Published article may be named, so a draft or
// archived fix is never handed to the next customer.

const (
	ServiceArticleDoctype     = "ServiceArticle"
	serviceArticleCodeInvalid = "CRMPLT-1131"
)

// validateServiceArticleSave: a Published article has its text.
func validateServiceArticleSave(payload map[string]interface{}) error {
	if strField(payload, "status") == "Published" && strField(payload, "body") == "" {
		return &ValidationError{Code: serviceArticleCodeInvalid, Message: "A published article needs its text."}
	}
	return nil
}

// validateTicketHelpArticle is the service ticket's step: a help article it
// names must exist and be Published when it is named. Blank is allowed, and a
// ticket whose article is archived later is not held up on its next save.
func validateTicketHelpArticle(tenantID string, priorData, payload map[string]interface{}) error {
	ref := strField(payload, "kb_article")
	if ref == "" || (priorData != nil && strField(priorData, "kb_article") == ref) {
		return nil
	}
	_, status, err := fetchDocData(tenantID, ServiceArticleDoctype, ref)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && status != "Published") {
		return &ValidationError{Code: serviceArticleCodeInvalid, Message: "Only a published help article can be named on a ticket."}
	}
	return err
}
