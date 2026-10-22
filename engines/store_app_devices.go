package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Stage 102.1.4 - the shared phone (decision D4, 2026-10-21; 49.14.3's
// floor-device posture). A store manager registers a phone to the store once;
// after that, each person signs in on it with their own PIN instead of the
// password and the two-factor code. The registered phone is the "something
// you have" (only a manager, fully signed in, can make one), the PIN the
// "something you know".
//
// The rules:
//   - The phone keeps a random token; the server keeps only its SHA-256 (the
//     mfa_trusted_devices.go pattern). A PIN works only together with the
//     token of a phone that is still registered.
//   - Store roles only. A full-access role (Admin, Super Admin) never signs in
//     with a PIN: its power is not for a phone passed around a counter.
//   - The person must be allowed to work at the phone's store (85), and the
//     session is that store's: the phone does not switch store.
//   - The PIN is hashed with bcrypt like a password, set only after re-typing
//     the password, and has its own lockout: 5 wrong PINs stop PIN sign-in
//     for that person for 15 minutes (the password still works). A locked or
//     password-locked account cannot use a PIN either.
//   - The session lasts a shift (12 hours) and names the phone, so removing
//     the phone (lost, stolen, retired) ends every session on it at once
//     (apiMiddleware checks it on each call). The app locks itself after the
//     client's store_app.pin_idle_minutes without use.

const (
	SettingStoreAppPinIdle  = "store_app.pin_idle_minutes"
	storeAppPinMaxFails     = 5
	storeAppPinLockMinutes  = 15
	storeAppPinSessionHours = 12
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingStoreAppPinIdle, Module: "Store app",
		Label: "Shared phone: lock after", Type: SettingTypeInt, Default: "5", Unit: "minutes",
		Min: settingBound(1), Max: settingBound(60),
		Description: "On a phone registered to a store, the store app signs the person out after this many minutes without use, so the next person signs in with their own PIN. A person's own shorter Auto Logout still applies.",
	})
}

// ErrStoreAppPINSignIn is the one answer every failed PIN sign-in gets - a
// wrong PIN, an unknown name, no PIN set, a locked PIN - the same reason the
// password login never says which part was wrong.
var ErrStoreAppPINSignIn = errors.New("wrong name or PIN")

// ErrStoreAppDeviceUnknown says the phone is not (or no longer) registered.
var ErrStoreAppDeviceUnknown = errors.New("this phone is not registered to a store")

// StoreAppDevice is a phone registered to a store.
type StoreAppDevice struct {
	ID           string `json:"id"`
	Store        string `json:"store"`
	StoreName    string `json:"store_name,omitempty"`
	Label        string `json:"label"`
	RegisteredBy string `json:"registered_by"`
	RegisteredAt string `json:"registered_at"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
	LastUser     string `json:"last_user,omitempty"`
}

// StoreAppManagerRole is the store app's manager: a store manager, or a
// full-access role. It registers phones and sees Today (102.2.1).
func StoreAppManagerRole(role string) bool {
	return HasFullAccess(role) || normalizeRoleKey(role) == normalizeRoleKey(RoleStoreManager)
}

// StoreAppMayManageDevices: who registers and removes store phones.
func StoreAppMayManageDevices(role string) bool { return StoreAppManagerRole(role) }

// StoreAppPINAllowed: store roles only, never a full-access role.
func StoreAppPINAllowed(role string) bool {
	return strings.TrimSpace(role) != "" && !HasFullAccess(role)
}

func storeAppTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func storeAppRandom(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func storeAppStoreName(schema, store string) string {
	name := store
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'name', ''), id) FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), store).Scan(&name)
	return name
}

// RegisterStoreAppDevice registers this phone to store and returns its token,
// shown to the phone once and never again.
func RegisterStoreAppDevice(tenantID, userID, username, role, store, label string) (*StoreAppDevice, string, error) {
	if !StoreAppMayManageDevices(role) {
		return nil, "", &ValidationError{Code: "GLOBAL-0002", Message: "only a store manager or an administrator can register a phone to a store"}
	}
	store, label = strings.TrimSpace(store), strings.TrimSpace(label)
	if store == "" {
		return nil, "", &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: "choose the store this phone belongs to first"}
	}
	if label == "" {
		label = "Store phone"
	}
	if len(label) > 160 {
		label = label[:160]
	}
	access, err := UserStoreAccess(tenantID, userID)
	if err != nil {
		return nil, "", err
	}
	if !access.Allows(store) {
		return nil, "", &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: fmt.Sprintf("you cannot work at %s, so you cannot register a phone to it", store)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL)`, schema), store).Scan(&exists); err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: fmt.Sprintf("store %s does not exist", store)}
	}
	token, err := storeAppRandom(32)
	if err != nil {
		return nil, "", err
	}
	idPart, err := storeAppRandom(9)
	if err != nil {
		return nil, "", err
	}
	id := "PHONE-" + strings.ToUpper(strings.NewReplacer("-", "X", "_", "Y").Replace(idPart))
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.store_app_devices (id, store, label, token_hash, registered_by) VALUES ($1, $2, $3, $4, $5)`, schema),
		id, store, label, storeAppTokenHash(token), firstNonEmpty(username, userID)); err != nil {
		return nil, "", err
	}
	LogAuditEvent(tenantID, firstNonEmpty(username, userID), "STORE_APP_DEVICE", "REGISTERED", fmt.Sprintf("Phone %s (%s) registered to store %s", id, label, store))
	d := &StoreAppDevice{ID: id, Store: store, StoreName: storeAppStoreName(schema, store), Label: label, RegisteredBy: firstNonEmpty(username, userID),
		RegisteredAt: time.Now().Format("2006-01-02 15:04")}
	return d, token, nil
}

const storeAppDeviceCols = `id, store, label, registered_by, to_char(registered_at, 'YYYY-MM-DD HH24:MI'), COALESCE(to_char(last_used_at, 'YYYY-MM-DD HH24:MI'), ''), COALESCE(last_user, '')`

func scanStoreAppDevice(row interface{ Scan(...interface{}) error }) (StoreAppDevice, error) {
	var d StoreAppDevice
	err := row.Scan(&d.ID, &d.Store, &d.Label, &d.RegisteredBy, &d.RegisteredAt, &d.LastUsedAt, &d.LastUser)
	return d, err
}

// StoreAppDeviceForToken is the registered phone holding token.
func StoreAppDeviceForToken(tenantID, token string) (*StoreAppDevice, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrStoreAppDeviceUnknown
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d, err := scanStoreAppDevice(db.DB.QueryRow(fmt.Sprintf(`SELECT `+storeAppDeviceCols+` FROM %s.store_app_devices WHERE token_hash = $1 AND removed_at IS NULL`, schema), storeAppTokenHash(token)))
	if err == sql.ErrNoRows {
		return nil, ErrStoreAppDeviceUnknown
	}
	if err != nil {
		return nil, err
	}
	d.StoreName = storeAppStoreName(schema, d.Store)
	return &d, nil
}

// StoreAppDeviceActive is apiMiddleware's per-call check of a PIN session's
// phone. A database failure reads as not active: the session can sign in again.
func StoreAppDeviceActive(tenantID, deviceID string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var active bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT removed_at IS NULL FROM %s.store_app_devices WHERE id = $1`, schema), deviceID).Scan(&active)
	return err == nil && active
}

// ListStoreAppDevices lists the phones registered to the stores this person
// may work at (every store for a full-access role).
func ListStoreAppDevices(tenantID, userID, role string) ([]StoreAppDevice, error) {
	if !StoreAppMayManageDevices(role) {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "only a store manager or an administrator can see the store phones"}
	}
	access, err := UserStoreAccess(tenantID, userID)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT `+storeAppDeviceCols+` FROM %s.store_app_devices WHERE removed_at IS NULL ORDER BY store, registered_at`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreAppDevice{}
	for rows.Next() {
		d, err := scanStoreAppDevice(rows)
		if err != nil {
			return nil, err
		}
		if HasFullAccess(role) || access.Allows(d.Store) {
			out = append(out, d)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].StoreName = storeAppStoreName(schema, out[i].Store)
	}
	return out, nil
}

// RemoveStoreAppDevice unregisters a phone; every PIN session on it ends.
func RemoveStoreAppDevice(tenantID, userID, username, role, deviceID string) error {
	if !StoreAppMayManageDevices(role) {
		return &ValidationError{Code: "GLOBAL-0002", Message: "only a store manager or an administrator can remove a store phone"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var store string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT store FROM %s.store_app_devices WHERE id = $1 AND removed_at IS NULL`, schema), deviceID).Scan(&store); err == sql.ErrNoRows {
		return &ValidationError{Code: "GLOBAL-0004", Message: "that phone is not registered (it may already have been removed)"}
	} else if err != nil {
		return err
	}
	if !HasFullAccess(role) {
		access, err := UserStoreAccess(tenantID, userID)
		if err != nil {
			return err
		}
		if !access.Allows(store) {
			return &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("that phone belongs to %s, a store you cannot work at", store)}
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.store_app_devices SET removed_at = NOW(), removed_by = $2 WHERE id = $1 AND removed_at IS NULL`, schema), deviceID, firstNonEmpty(username, userID)); err != nil {
		return err
	}
	LogAuditEvent(tenantID, firstNonEmpty(username, userID), "STORE_APP_DEVICE", "REMOVED", fmt.Sprintf("Phone %s removed from store %s; its PIN sessions ended", deviceID, store))
	return nil
}

// storeAppWeakPIN refuses PINs anyone would try first.
func storeAppWeakPIN(pin string) bool {
	same, up, down := true, true, true
	for i := 1; i < len(pin); i++ {
		if pin[i] != pin[0] {
			same = false
		}
		if pin[i] != pin[i-1]+1 {
			up = false
		}
		if pin[i] != pin[i-1]-1 {
			down = false
		}
	}
	return same || up || down
}

// SetStoreAppPIN sets (or changes) a person's PIN. The password is asked again,
// so an unattended signed-in screen cannot be turned into a PIN.
func SetStoreAppPIN(tenantID, userID, role, password, pin string) error {
	if !StoreAppPINAllowed(role) {
		return &ValidationError{Code: "GLOBAL-0002", Message: "a PIN is for store roles; an administrator signs in with the password and the two-factor code"}
	}
	pin = strings.TrimSpace(pin)
	if len(pin) < 4 || len(pin) > 8 || strings.Trim(pin, "0123456789") != "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "pin", Message: "the PIN must be 4 to 8 digits"}
	}
	if storeAppWeakPIN(pin) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "pin", Message: "choose a PIN that is not all one digit or a run like 1234"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var hash string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id = $1 AND status = 'Active'`, schema), userID).Scan(&hash); err != nil {
		return &ValidationError{Code: "GLOBAL-0002", Message: "your account could not be read"}
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "password", Message: "the password is not right"}
	}
	pinHash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.store_app_pins (user_id, pin_hash) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET pin_hash = EXCLUDED.pin_hash, failed_count = 0, locked_until = NULL, updated_at = NOW()`, schema),
		userID, string(pinHash)); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "STORE_APP_PIN", "SET", "Store app PIN set")
	return nil
}

// ClearStoreAppPIN removes a person's PIN.
func ClearStoreAppPIN(tenantID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.store_app_pins WHERE user_id = $1`, schema), userID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "STORE_APP_PIN", "REMOVED", "Store app PIN removed")
	return nil
}

// StoreAppPINStatus says whether this person has a PIN and may have one.
func StoreAppPINStatus(tenantID, userID, role string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var set bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.store_app_pins WHERE user_id = $1)`, schema), userID).Scan(&set); err != nil {
		return nil, err
	}
	return map[string]interface{}{"set": set, "allowed": StoreAppPINAllowed(role)}, nil
}

// StoreAppPINSession is a successful PIN sign-in.
type StoreAppPINSession struct {
	Token       string `json:"token"`
	User        string `json:"user"`
	Role        string `json:"role"`
	Store       string `json:"store"`
	StoreName   string `json:"store_name"`
	IdleMinutes int    `json:"idle_minutes"`
}

// StoreAppPINSignIn signs a person in on a registered phone with their PIN.
func StoreAppPINSignIn(tenantID, deviceToken, username, pin string) (*StoreAppPINSession, error) {
	device, err := StoreAppDeviceForToken(tenantID, deviceToken)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, role, uname string
	var cv int
	var accountLocked bool
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, role, username, credential_version, (locked_until IS NOT NULL AND locked_until > NOW())
		FROM %s.users WHERE username = $1 AND status = 'Active'`, schema), strings.TrimSpace(username)).Scan(&id, &role, &uname, &cv, &accountLocked)
	if err != nil {
		return nil, ErrStoreAppPINSignIn
	}
	if accountLocked || !StoreAppPINAllowed(role) {
		return nil, ErrStoreAppPINSignIn
	}
	var pinHash string
	var fails int
	var pinLocked bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT pin_hash, failed_count, (locked_until IS NOT NULL AND locked_until > NOW()) FROM %s.store_app_pins WHERE user_id = $1`, schema), id).
		Scan(&pinHash, &fails, &pinLocked)
	if err != nil || pinLocked {
		return nil, ErrStoreAppPINSignIn
	}
	if bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(strings.TrimSpace(pin))) != nil {
		if fails+1 >= storeAppPinMaxFails {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.store_app_pins SET failed_count = 0, locked_until = NOW() + make_interval(mins => $2) WHERE user_id = $1`, schema), id, storeAppPinLockMinutes)
			LogAuditEvent(tenantID, uname, "LOGIN", "PIN_LOCKED", fmt.Sprintf("%d wrong PINs on phone %s; PIN sign-in stopped for %d minutes", storeAppPinMaxFails, device.ID, storeAppPinLockMinutes))
		} else {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.store_app_pins SET failed_count = failed_count + 1 WHERE user_id = $1`, schema), id)
		}
		return nil, ErrStoreAppPINSignIn
	}
	live, err := ResolveLiveUserState(tenantID, id)
	if err != nil {
		return nil, ErrStoreAppPINSignIn
	}
	if !live.Access.Allows(device.Store) || live.MustChangePassword {
		return nil, ErrStoreAppPINSignIn
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.store_app_pins SET failed_count = 0, locked_until = NULL WHERE user_id = $1`, schema), id)
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.store_app_devices SET last_used_at = NOW(), last_user = $2 WHERE id = $1`, schema), device.ID, uname)
	LogAuditEvent(tenantID, uname, "LOGIN", "PIN_SUCCESS", fmt.Sprintf("Signed in with a PIN on phone %s at store %s", device.ID, device.Store))
	idle := GetSettingInt(tenantID, SettingStoreAppPinIdle)
	if idle <= 0 {
		idle = 5
	}
	return &StoreAppPINSession{
		Token: SignStoreDeviceToken(id, uname, live.Role, tenantID, device.Store, cv, device.ID, storeAppPinSessionHours*time.Hour),
		User:  uname, Role: CanonicalRole(live.Role), Store: device.Store, StoreName: device.StoreName, IdleMinutes: idle,
	}, nil
}
