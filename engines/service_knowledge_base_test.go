package engines

import "testing"

// Ask 176.5: service knowledge base - a published article has its text, and
// a ticket names only a published article.

func TestStage1765PublishedArticleNeedsItsText(t *testing.T) {
	f := g170Fix(t)
	wantCode(t, ValidateTransactionalRules(f.tenantID, "ServiceArticle", "", "", nil, map[string]interface{}{"title": "Reset", "status": "Published"}), "CRMPLT-1131")
	for _, p := range []map[string]interface{}{
		{"title": "Reset", "status": "Draft"},
		{"title": "Reset", "status": "Published", "body": "Hold the button 10 seconds."},
	} {
		if err := ValidateTransactionalRules(f.tenantID, "ServiceArticle", "", "", nil, p); err != nil {
			t.Fatalf("%v was refused: %v", p, err)
		}
	}
}

func TestStage1765TicketNamesOnlyAPublishedArticle(t *testing.T) {
	f := g170Fix(t)
	f.insert("KB-D", "ServiceArticle", "Draft", "system", map[string]interface{}{"code": "KB-D", "title": "Draft fix"})
	f.insert("KB-P", "ServiceArticle", "Published", "system", map[string]interface{}{"code": "KB-P", "title": "Known fix", "body": "Steps"})
	f.insert("KB-A", "ServiceArticle", "Archived", "system", map[string]interface{}{"code": "KB-A", "title": "Old fix", "body": "Steps"})
	ticket := func(kb string) map[string]interface{} {
		return map[string]interface{}{"subject": "AC", "kb_article": kb}
	}

	for _, kb := range []string{"KB-D", "KB-A", "KB-NOPE"} {
		wantCode(t, validateTicketHelpArticle(f.tenantID, nil, ticket(kb)), "CRMPLT-1131")
	}
	for _, kb := range []string{"KB-P", ""} {
		if err := validateTicketHelpArticle(f.tenantID, nil, ticket(kb)); err != nil {
			t.Fatalf("%q was refused: %v", kb, err)
		}
	}
	// A ticket whose article was archived after it was named is not held up.
	if err := validateTicketHelpArticle(f.tenantID, ticket("KB-A"), ticket("KB-A")); err != nil {
		t.Fatalf("an unchanged archived article blocked the ticket: %v", err)
	}
	if err := ValidateTransactionalRules(f.tenantID, "ServiceTicket", "", "", nil, ticket("KB-D")); err == nil {
		t.Fatal("the ticket save did not run the article check")
	}
}
