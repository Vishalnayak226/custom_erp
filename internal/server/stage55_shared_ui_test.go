package server

import (
	"regexp"
	"strings"
	"testing"
)

// Structural tripwires supplement (not replace) the real click/typing/retry/
// persistence checks in docs/qa/stage55-shared-correctness.cjs.
func TestStage55SharedFormEntryContracts(t *testing.T) {
	docs := readPublicAsset(t, "view-documents.js")
	for _, required := range []string{
		"addEventListener('click', () => openDynamicModal())",
		"typeof existingRecord.id === 'string'",
		"editingDocID = isEdit ? existingRecord.id : null",
		"firstInput || modal.querySelector('.modal-close')",
		"e.shiftKey ? last : first",
		"Open Users for password reset",
		"doc-reset-users-button",
	} {
		if !strings.Contains(docs, required) {
			t.Errorf("missing shared form contract %q", required)
		}
	}
	if strings.Contains(docs, "addEventListener('click', openDynamicModal)") {
		t.Error("a click event is being passed as an existing record again")
	}
	html := readPublicAsset(t, "index.html")
	if !strings.Contains(html, `role="dialog" aria-modal="true" aria-labelledby="dynamic-modal-title"`) {
		t.Error("dynamic form must expose its dialog role and accessible title")
	}
}

func TestStage55EmbeddedRecordTabsUseRetryableLazyLoader(t *testing.T) {
	for _, file := range []string{"view-hr.js", "view-manufacturing.js"} {
		body := readPublicAsset(t, file)
		if strings.Contains(body, "await renderDocTableView(container)") {
			t.Errorf("%s assumes the records chunk has already loaded", file)
		}
		if !strings.Contains(body, "container, LAZY_VIEW_MODULES['doctype-table']") {
			t.Errorf("%s must use the shared retryable records loader", file)
		}
	}
}

func TestStage55PhoneDecoratorHonorsSemanticMetadata(t *testing.T) {
	app := readPublicAsset(t, "app.js")
	function := regexp.MustCompile(`(?s)function isPhoneFieldName\(name\) \{(.*?)\n\}`).FindStringSubmatch(app)
	if len(function) != 2 {
		t.Fatal("phone field matcher not found")
	}
	for _, required := range []string{"fieldSemantic(n)", "return semantic === 'phone'", "!Object.keys(FIELD_SEMANTICS).length"} {
		if !strings.Contains(function[1], required) {
			t.Errorf("country phone decorator missing semantic safeguard %q", required)
		}
	}
}

func TestStage55ShellAndViewModuleCacheVersionsAgree(t *testing.T) {
	app := readPublicAsset(t, "app.js")
	html := readPublicAsset(t, "index.html")
	shell := regexp.MustCompile(`/app\.js\?v=(\d+)`).FindStringSubmatch(html)
	chunks := regexp.MustCompile(`const VIEW_MODULE_VERSION = '(\d+)'`).FindStringSubmatch(app)
	if len(shell) != 2 || len(chunks) != 2 || shell[1] != chunks[1] {
		t.Fatalf("shell and lazy chunks need the same release cache version: shell=%v chunks=%v", shell, chunks)
	}
}
