package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 36.4.4: PIMCatalog exposes a PIMProductGroup as a shareable,
// tokenised, read-only link - Unbxd's "Catalogs". The group is re-resolved
// (ResolvePIMProductGroup, the 36.1.3 seam) on every view rather than
// materialised once, so the catalogue a partner opens is always the group's
// live membership, not a stale snapshot. The token itself is never stored -
// only its SHA-256 digest, minted/rotated by
// POST /api/v1/pim/catalogs/{id}/rotate-share-token, the same crypto/rand +
// digest-only shape Stage 36.3.4's import hook token and Stage 38.2a's API
// keys both already use. This is deliberately the SAME auth primitive this
// codebase already relies on twice, not a second auth system: an opaque
// bearer secret, hashed at rest, checked in constant time.

// ValidatePIMCatalogDocument runs at ValidateDocument's shared exit.
func ValidatePIMCatalogDocument(tenantID string, payload map[string]interface{}) error {
	group := strings.TrimSpace(pimString(payload["product_group"]))
	if group == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Product Group", Message: "a catalog needs a product group"}
	}
	if db.DB != nil {
		if _, _, err := fetchPIMProductGroup(tenantID, group); err != nil {
			return &ValidationError{Code: "META-0198", SubFor: "Product Group", Message: err.Error()}
		}
	}
	if err := validateISODate("Expiry Date", pimString(payload["expiry_date"]), false); err != nil {
		return err
	}
	return nil
}

func fetchPIMCatalogData(tenantID, catalogID string) (canonicalID string, data map[string]interface{}, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PIMCatalog' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), catalogID).Scan(&canonicalID, &raw)
	if err != nil {
		return "", nil, fmt.Errorf("catalog %q not found", catalogID)
	}
	if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
		return "", nil, fmt.Errorf("catalog %q has invalid stored data: %w", catalogID, uErr)
	}
	return canonicalID, data, nil
}

// PIMCatalogInfo is the list-facing view - never carries share_token_hash,
// the same "the digest itself has no business on a list screen" posture
// PIMImportScheduleInfo already takes.
type PIMCatalogInfo struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	ProductGroup string `json:"product_group"`
	ExpiryDate   string `json:"expiry_date,omitempty"`
	HasShareLink bool   `json:"has_share_link"`
	LastSharedAt string `json:"last_shared_at,omitempty"`
	Status       string `json:"status"`
}

// ListPIMCatalogs backs the Catalogs authoring screen.
func ListPIMCatalogs(tenantID string) ([]PIMCatalogInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMCatalog' AND deleted_at IS NULL ORDER BY COALESCE(data->>'name', id)`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMCatalogInfo{}
	for rows.Next() {
		var id, raw, status string
		if sErr := rows.Scan(&id, &raw, &status); sErr != nil {
			return nil, sErr
		}
		var data map[string]interface{}
		if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
			continue
		}
		out = append(out, PIMCatalogInfo{
			ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
			ProductGroup: pimString(data["product_group"]), ExpiryDate: pimString(data["expiry_date"]),
			HasShareLink: pimString(data["share_token_hash"]) != "", LastSharedAt: pimString(data["last_shared_at"]),
			Status: status,
		})
	}
	return out, rows.Err()
}

// RotatePIMCatalogShareToken mints a fresh 256-bit token, stores only its
// SHA-256 digest, and returns the raw token exactly once. Rotating
// immediately invalidates whatever link was shared before it - the correct
// response to a link leaking to the wrong audience.
func RotatePIMCatalogShareToken(tenantID, catalogID string) (rawToken string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	canonicalID, _, err := fetchPIMCatalogData(tenantID, catalogID)
	if err != nil {
		return "", err
	}

	buf := make([]byte, 32)
	if _, rErr := rand.Read(buf); rErr != nil {
		return "", rErr
	}
	rawToken = hex.EncodeToString(buf)
	digest := sha256.Sum256([]byte(rawToken))

	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{share_token_hash}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'PIMCatalog' AND id = $2`, schema), hex.EncodeToString(digest[:]), canonicalID)
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

// resolvePIMCatalogIDByShareToken hashes the caller-supplied token and finds
// the (unique, Active, unexpired) catalog whose stored digest matches,
// scoped to the caller's own tenant. A caller naming the wrong tenant, an
// unknown token, a deactivated catalog and an expired one are all
// indistinguishable "not found" - which of those is true is not information
// this public endpoint leaks.
func resolvePIMCatalogIDByShareToken(tenantID, rawToken string) (catalogID string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(rawToken))
	today := time.Now().Format("2006-01-02")

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'share_token_hash',''), COALESCE(data->>'expiry_date','')
		FROM %s.documents WHERE doctype = 'PIMCatalog' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, storedHex, expiry string
		if sErr := rows.Scan(&id, &storedHex, &expiry); sErr != nil {
			continue
		}
		if storedHex == "" {
			continue
		}
		stored, decErr := hex.DecodeString(storedHex)
		if decErr != nil {
			continue
		}
		if subtle.ConstantTimeCompare(stored, digest[:]) == 1 {
			if expiry != "" && expiry < today {
				return "", fmt.Errorf("this catalog share link has expired")
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("no active catalog matches this share link")
}

// PIMCatalogShareProduct is the curated, read-only projection a partner
// sees - the same "no cost/margin/internal-only data" discipline Stage
// 38.1's public API response types apply, here reusing the search-feed
// reader's own field set rather than a document dump.
type PIMCatalogShareProduct struct {
	ItemCode     string `json:"item_code"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	ShortDesc    string `json:"short_desc"`
	Category     string `json:"category"`
	HasMainImage bool   `json:"has_main_image"`
}

type PIMCatalogShareView struct {
	CatalogName string                   `json:"catalog_name"`
	ResolvedAt  time.Time                `json:"resolved_at"`
	Products    []PIMCatalogShareProduct `json:"products"`
}

// GetPIMCatalogShareView is the one call the public catalog-share handler
// makes once a token has resolved to a catalog id: re-resolve the group live,
// project onto the curated shape above, and best-effort stamp
// last_shared_at (a write failure here must not fail the read a partner is
// waiting on).
func GetPIMCatalogShareView(tenantID, catalogID string) (*PIMCatalogShareView, error) {
	canonicalID, data, err := fetchPIMCatalogData(tenantID, catalogID)
	if err != nil {
		return nil, err
	}
	group := pimString(data["product_group"])
	resolved, err := ResolvePIMProductGroup(tenantID, group)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(resolved.Members))
	for _, m := range resolved.Members {
		codes = append(codes, m.ItemCode)
	}
	feed, err := fetchSearchFeedRows(tenantID, codes)
	if err != nil {
		return nil, err
	}
	view := &PIMCatalogShareView{CatalogName: pimString(data["name"]), ResolvedAt: time.Now().UTC(), Products: []PIMCatalogShareProduct{}}
	for _, row := range feed {
		view.Products = append(view.Products, PIMCatalogShareProduct{
			ItemCode: row.ItemCode, Name: row.Name, Title: row.Title,
			ShortDesc: row.ShortDesc, Category: row.Category, HasMainImage: row.HasMainImage,
		})
	}

	if schema, sErr := db.GetTenantSchema(tenantID); sErr == nil {
		_, _ = db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{last_shared_at}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'PIMCatalog' AND id = $2`, schema), time.Now().UTC().Format(time.RFC3339), canonicalID)
	}
	return view, nil
}

// ResolvePIMCatalogShareToken is the public handler's one entry point:
// resolve a raw token straight to a curated share view, without exposing the
// intermediate catalog id to the caller.
func ResolvePIMCatalogShareToken(tenantID, rawToken string) (*PIMCatalogShareView, error) {
	catalogID, err := resolvePIMCatalogIDByShareToken(tenantID, rawToken)
	if err != nil {
		return nil, err
	}
	return GetPIMCatalogShareView(tenantID, catalogID)
}
