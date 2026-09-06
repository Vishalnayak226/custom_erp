//go:build stage47redteam

package server

// Stage 47.0.1 - red-team regression/abuse scenarios for audit findings A-01
// through A-07 (docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md, lines
// 63-132; tracked at docs/micro_checklist.md Stage 47, item 47.0.1).
//
// HOW TO RUN:
//
//	go test -tags stage47redteam ./engines/... ./internal/server/...
//
// or narrower, e.g.:
//
//	go test -tags stage47redteam ./internal/server/... -run TestA01
//
// These tests assert the SECURE/CORRECT outcome for each finding - the
// outcome the audit says the product does NOT currently deliver. They are
// EXPECTED TO FAIL (red) against the codebase as it stands on 2026-09-03.
// This item (47.0.1) is explicitly scoped to freezing evidence, not fixing
// it - see the item's own text: "no product behavior change in this item."
//
// A PASSING result for a given finding's test means that finding has been
// remediated (by 47.1-47.7, whichever owns that area) - at that point
// whichever session closes the corresponding 47.x item should promote that
// test out of this build tag into the ordinary suite, per 47.0.1's own
// closure note.
//
// The stage47redteam tag keeps these off the default `go test ./...` path
// so a deliberately-failing security assertion never breaks the build for a
// concurrent session or CI sharing this tree - see docs/micro_checklist.md's
// Stage 47.0.1 entry and the top-level CLAUDE.md's shared-tree note.
//
// This file is the suite's documentation. The helpers it used to define moved
// to stage47_redteam_helpers_shared_test.go (untagged) when 47.2 promoted the
// A-02 test out of this build tag - a promoted test must compile on the default
// `go test ./...` path, and it cannot if the helpers it calls are behind a tag.
