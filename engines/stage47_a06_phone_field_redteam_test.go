//go:build stage47redteam

package engines

import "testing"

// Stage 47.0.1 / audit finding A-06 ("Core WMS mobile workflows are not
// operable on a phone-sized device" -
// docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md lines 109-115). One
// concrete, code-provable piece of that finding: IsPhoneField's substring
// heuristic (engines/phone.go:455, phoneFieldTokens including "mobile")
// misclassifies any WMS field whose name merely contains "mobile" as a
// phone number - the audit's own example is a Wave ID field named
// "mobile-pick-wave-id", which then gets a telephone keyboard, digit-only
// stripping and an India-phone-length limit, making an alphanumeric wave ID
// impossible to enter.
//
// This is deliberately in the stage47redteam-tagged suite alongside the
// authorization findings, not the default `go test ./...` path, even though
// it isn't itself a security assertion - see stage47_redteam_helpers_test.go
// (internal/server) for why: a test that currently fails cannot live in the
// default build without breaking it for every concurrent session sharing
// this tree. Run via `go test -tags stage47redteam ./engines/...`.
//
// Required closure (47.6.1): explicit semantic field metadata (text, phone,
// barcode, wave, LPN, lot, serial, numeric/date) replacing this substring
// inference for WMS identifier fields.
func TestA06WaveIDFieldNotMisclassifiedAsPhone(t *testing.T) {
	cases := []string{
		"mobile-pick-wave-id",
		"mobile_pick_wave_id",
		"mobilePickWaveId",
	}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			if IsPhoneField(field) {
				t.Fatalf("A-06: IsPhoneField(%q) = true - a WMS wave-ID field is being classified as a phone number purely because its name contains \"mobile\", which is exactly the audit's reproduced bug (telephone keyboard + digit-stripping + India phone-length limit applied to an alphanumeric wave ID). Must be false once 47.6.1's explicit field-type metadata replaces this substring heuristic for identifier fields.", field)
			}
		})
	}
}
