package engines

// Stage 73.D.9: records that belong to whoever created them.
//
// A scheduled report or dashboard digest holds its owner's recipient emails
// and webhook URL (with its token). Store Manager holds read / create / update
// on both doctypes, so through the record API every store manager could read
// and change every other owner's schedule. The rule here is the one the
// Schedules tab already applies (report_schedule_view.go): the creator, or a
// Super Admin, and nobody else - for list, open, edit and delete alike.
//
// 165.1: UserShortcut (a person's favourites and saved views) is creator-owned
// for the same reason: the screens pass ?owner=<me>, but that is the caller's
// own filter, so the server confines every role to the rows it created. That
// is what lets RoleMayOnDoctype give every role its own favourites.
var creatorOwnedDoctypes = map[string]bool{"ScheduledReport": true, "DashboardDigest": true, UserShortcutDoctype: true}

// UserShortcutDoctype is the favourites / saved-views record type (Stage 139).
const UserShortcutDoctype = "UserShortcut"

// CreatorOwnedDoctype reports whether doctype's records are their creator's.
func CreatorOwnedDoctype(doctype string) bool { return creatorOwnedDoctypes[doctype] }

// CreatorMayAccess reports whether this session may see or change one record
// of doctype that createdBy (documents.created_by, a user id) created.
func CreatorMayAccess(doctype, role, userID, createdBy string) bool {
	if !creatorOwnedDoctypes[doctype] || HasFullAccess(role) {
		return true
	}
	return userID != "" && createdBy == userID
}

// ReportColumnProfileDoctype is a saved column layout: a report's (Stage 28.3)
// or a list's (276.2, report_id "list:<list>"). Scope Personal is one user's
// own choice (owner = username); Universal is the default for everyone.
const ReportColumnProfileDoctype = "ReportColumnProfile"

// ColumnProfileManager reports whether role sets the default for everyone
// and may change anyone's column layout: a full-access role or a Store Manager.
func ColumnProfileManager(role string) bool {
	return HasFullAccess(role) || role == RoleStoreManager
}

// ColumnProfileWriteAllowed (276.2) reports whether role, signed in as
// username, may save or delete this column profile - checked on the stored
// record and again on the record as sent. Every role keeps its own column
// choices (RoleMayOnDoctype), so a role that does not manage them writes only
// a Personal profile that is its own.
func ColumnProfileWriteAllowed(role, username string, record map[string]interface{}) bool {
	if ColumnProfileManager(role) || record == nil {
		return true
	}
	scope, _ := record["scope"].(string)
	owner, _ := record["owner"].(string)
	return username != "" && scope != "Universal" && owner == username
}
