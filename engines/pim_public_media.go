package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"custom_erp/db"
)

// Stage 146.5.9: a stable public image address for marketplace feeds.
//
// A marketplace has to fetch each picture itself, with no login. This serves
// one ProductMedia image at /media/{client}/{media id}/{signature}, where the
// signature is an HMAC-SHA256, made with a key only the server holds, over the
// client and the media id. A guessed or altered address is a plain 404; so is
// everything else that is not a live, active image of a client that switched
// the setting on. The media id carries the file's checksum (generateMediaID),
// so one address always names the same bytes and may be cached for good.

// SettingPublicMediaURLs switches public image addresses on for a client. Off
// by default: a feed's image_url then fills only from a media path that is
// already a public web address, as before.
const SettingPublicMediaURLs = "pim.public_media_urls"

// Environment variables holding the signing key and, while a key is being
// rotated, the one it replaces (accepted for checking only, never for signing).
const (
	PublicMediaKeyEnv         = "PUBLIC_MEDIA_SIGNING_KEY"
	PublicMediaPreviousKeyEnv = "PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS"
)

// publicMediaMinKeyLen is the shortest key the feature accepts; a shorter one
// counts as not set.
const publicMediaMinKeyLen = 32

// ErrPublicMediaNotFound is the one answer for every reason an address does not
// serve: bad signature, unknown or deleted image, setting off, key unset. The
// caller must not tell them apart.
var ErrPublicMediaNotFound = errors.New("not found")

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingPublicMediaURLs, Module: "PIM / PIMS",
		Label: "Public image addresses for marketplace feeds", Type: SettingTypeBool, Default: "false",
		Description: "On: marketplace feed exports fill image_url with a signed public address for each picture kept in the ERP, and anyone holding such an address can fetch that one picture without signing in. Needs the server's PUBLIC_MEDIA_SIGNING_KEY (at least 32 characters) and a web address (Web address for customer links, or the client's own host name). Off: only media that already has a public web address reaches a feed.",
	})
	settingChangeHooks[SettingPublicMediaURLs] = func(tenantID, from, to, by string) error {
		if strings.EqualFold(strings.TrimSpace(to), "true") && len(publicMediaVerifyKeys()) == 0 {
			return &ValidationError{Message: "Public image addresses cannot be switched on: this server has no signing key. Ask whoever runs the server to set " + PublicMediaKeyEnv + " (at least 32 characters) and restart it."}
		}
		return nil
	}
}

func publicMediaKey(previous bool) []byte {
	v := strings.TrimSpace(os.Getenv("PUBLIC_MEDIA_SIGNING_KEY"))
	if previous {
		v = strings.TrimSpace(os.Getenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS"))
	}
	if len(v) < publicMediaMinKeyLen {
		return nil
	}
	return []byte(v)
}

// publicMediaVerifyKeys is the current key, then the previous one (rotation).
func publicMediaVerifyKeys() [][]byte {
	var keys [][]byte
	if k := publicMediaKey(false); k != nil {
		keys = append(keys, k)
		if p := publicMediaKey(true); p != nil {
			keys = append(keys, p)
		}
	}
	// A previous key alone, with no current one, never serves: the feature is off.
	return keys
}

// publicMediaSignature is the signature part of an address.
func publicMediaSignature(key []byte, tenantID, mediaID string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("public-media-v1\n" + tenantID + "\n" + mediaID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// PublicMediaEnabled is true when the client switched public addresses on and
// the server holds a signing key.
func PublicMediaEnabled(tenantID string) bool {
	return len(publicMediaVerifyKeys()) > 0 && GetSettingBool(tenantID, SettingPublicMediaURLs)
}

// PublicMediaPath is /media/{client}/{media id}/{signature}, signed with the
// current key; "" when there is no key.
func PublicMediaPath(tenantID, mediaID string) string {
	key := publicMediaKey(false)
	if key == nil || tenantID == "" || mediaID == "" {
		return ""
	}
	return "/media/" + url.PathEscape(tenantID) + "/" + url.PathEscape(mediaID) + "/" + publicMediaSignature(key, tenantID, mediaID)
}

// publicMediaTypes are the only types the public route serves.
var publicMediaTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}

// ReadPublicMedia returns the image an address names, or ErrPublicMediaNotFound
// for any reason it must not serve it. Signature first (no database work for a
// forged address), then the client, the setting and the record.
func ReadPublicMedia(tenantID, mediaID, sig string) ([]byte, string, error) {
	keys := publicMediaVerifyKeys()
	if len(keys) == 0 || tenantID == "" || mediaID == "" || sig == "" {
		return nil, "", ErrPublicMediaNotFound
	}
	given := []byte(sig)
	valid := false
	for _, k := range keys { // every key is tried, so timing does not say which one matched
		if hmac.Equal(given, []byte(publicMediaSignature(k, tenantID, mediaID))) {
			valid = true
		}
	}
	if !valid {
		return nil, "", ErrPublicMediaNotFound
	}
	// A purged client's id would resolve to the default schema; refuse it.
	if ok, err := TenantIsOperational(tenantID); err != nil || !ok {
		return nil, "", ErrPublicMediaNotFound
	}
	if !GetSettingBool(tenantID, SettingPublicMediaURLs) {
		return nil, "", ErrPublicMediaNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", ErrPublicMediaNotFound
	}
	var live bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents
		WHERE doctype = 'ProductMedia' AND id = $1 AND deleted_at IS NULL AND status = 'Active')`, schema), mediaID).Scan(&live); err != nil || !live {
		return nil, "", ErrPublicMediaNotFound
	}
	b, fileType, _, err := ReadMediaFile(tenantID, mediaID)
	if err != nil || !publicMediaTypes[fileType] {
		return nil, "", ErrPublicMediaNotFound
	}
	return b, fileType, nil
}
