package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"custom_erp/db"
)

// Stage 130.8 (72.D h / 84.6, user decision 2026-10-22: encrypt). A gift
// card's number is a bearer credential - whoever holds it spends the balance -
// so it is no longer stored as typed. stored_value_instruments.card_number
// holds a keyed hash for lookups ("h1:<key id>:<hex>"), card_enc the number
// encrypted so an administrator can show it again for a lost card, and
// card_last4 what every screen prints. The keys are STORED_VALUE_KEY's
// rotation keyring (secret_keyring.go): every key in the ring still finds the
// cards it wrote, the newest writes new ones. Customer advances keep their CA
// document number - a reference, not a credential.

var svKeys, svSigningKey = loadVersionedKeyring("STORED_VALUE_KEY", "stored_value_key.local")

const svHashPrefix = "h1:"

func svHashWith(k versionedKey, number string) string {
	m := hmac.New(sha256.New, k.key)
	m.Write([]byte("stored-value-card|" + number))
	return svHashPrefix + k.id + ":" + hex.EncodeToString(m.Sum(nil))
}

// svLookupKeys are the card_number values a typed number may be stored
// under: its hash under every key in the ring, and the number itself for a
// card written before 130.8 that the boot backfill has not reached.
func svLookupKeys(number string) []string {
	out := make([]string, 0, len(svKeys)+1)
	for _, k := range svKeys {
		out = append(out, svHashWith(k, number))
	}
	return append(out, number)
}

// svLast4 is what a card prints as: its last four characters, or nothing for
// a number too short to show any of it.
func svLast4(number string) string {
	if len(number) <= 4 {
		return ""
	}
	return number[len(number)-4:]
}

// svSealCard is what a new card's row holds: the lookup hash, the last four
// and the encrypted number.
func svSealCard(number string) (key, last4, enc string, err error) {
	ct, err := encryptVersioned(svSigningKey, []byte(number))
	if err != nil {
		return "", "", "", err
	}
	return svHashWith(svSigningKey, number), svLast4(number), base64.StdEncoding.EncodeToString(ct), nil
}

// svMasked is how a card shows: "**** 1234" from its last four, else masked
// from its stored number (a customer advance's CA number, or a card the
// backfill has not reached).
func (c svCard) masked() string {
	if c.Last4 != "" {
		return "**** " + c.Last4
	}
	if strings.HasPrefix(c.Number, svHashPrefix) {
		return "****"
	}
	return MaskCardNumber(c.Number)
}

// svShown masks a card as a query returned it: "**** 1234" stays as it is.
func svShown(card string) string {
	if strings.HasPrefix(card, "**** ") {
		return card
	}
	return MaskCardNumber(card)
}

// svCryptoReady says whether a client's table has the 130.8 columns.
func svCryptoReady(schema string) bool {
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'stored_value_instruments'
		AND column_name IN ('card_enc', 'card_last4')`, schema).Scan(&n)
	return n == 2
}

// EncryptStoredValueCards seals every gift card of one client still stored as
// typed. Idempotent: a sealed card is skipped. Returns how many it sealed.
func EncryptStoredValueCards(schema string) (int, error) {
	if !svCryptoReady(schema) {
		return 0, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT instrument_id, card_number FROM %s.stored_value_instruments
		WHERE card_enc IS NULL AND profile <> $1 AND card_number NOT LIKE 'h1:%%'`, schema), AdvanceProfileID)
	if err != nil {
		return 0, err
	}
	type plain struct{ id, number string }
	var todo []plain
	for rows.Next() {
		var p plain
		if err := rows.Scan(&p.id, &p.number); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, p)
	}
	rows.Close()
	sealed := 0
	for _, p := range todo {
		key, last4, enc, err := svSealCard(p.number)
		if err != nil {
			return sealed, err
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.stored_value_instruments SET card_number = $2, card_last4 = $3, card_enc = $4
			WHERE instrument_id = $1 AND card_enc IS NULL`, schema), p.id, key, last4, enc); err != nil {
			return sealed, err
		}
		sealed++
	}
	return sealed, nil
}

// EncryptStoredValueCardsOnBoot seals what every client still holds as typed
// - run once as the server starts, before it serves a request.
func EncryptStoredValueCardsOnBoot() {
	if db.DB == nil {
		return
	}
	schemas, err := listTenantSchemas()
	if err != nil {
		log.Printf("[GIFTCARD] could not list clients to seal card numbers: %v", err)
		return
	}
	for _, schema := range schemas {
		if n, err := EncryptStoredValueCards(schema); err != nil {
			log.Printf("[GIFTCARD] %s: sealing card numbers stopped: %v", schema, err)
		} else if n > 0 {
			log.Printf("[GIFTCARD] %s: %d card number(s) now stored encrypted", schema, n)
		}
	}
}

// RevealStoredValueCard shows a gift card's full number again - for a lost
// card's reprint - to an administrator only, and records who looked.
func RevealStoredValueCard(tenantID, role, user, instrumentID string) (map[string]string, error) {
	if !HasFullAccess(role) {
		return nil, &ValidationError{Code: "GLOBAL-0011", Message: "only an administrator can show a gift card's full number"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var number, enc, profile string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT card_number, COALESCE(card_enc, ''), profile FROM %s.stored_value_instruments WHERE instrument_id = $1`, schema),
		strings.TrimSpace(instrumentID)).Scan(&number, &enc, &profile)
	if err != nil {
		return nil, &ValidationError{Code: "POSOFF-0431", SubFor: instrumentID, Message: fmt.Sprintf("no gift card has the id %q - take it from the Gift Card Liability report", instrumentID)}
	}
	if enc != "" {
		ct, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return nil, err
		}
		plain, err := decryptVersioned(svKeys, ct)
		if err != nil {
			return nil, fmt.Errorf("card %s could not be decrypted - its key is not in STORED_VALUE_KEY's ring: %v", instrumentID, err)
		}
		number = string(plain)
	}
	LogAuditEvent(tenantID, user, "REVEAL_GIFT_CARD_NUMBER", "SUCCESS", fmt.Sprintf("Full number of gift card %s (%s) shown", instrumentID, MaskCardNumber(number)))
	return map[string]string{"instrument_id": instrumentID, "profile": profile, "card_number": number, "card": MaskCardNumber(number)}, nil
}
