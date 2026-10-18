package engines

// Stage 73.D.9: records that belong to whoever created them.
//
// A scheduled report or dashboard digest holds its owner's recipient emails
// and webhook URL (with its token). Store Manager holds read / create / update
// on both doctypes, so through the record API every store manager could read
// and change every other owner's schedule. The rule here is the one the
// Schedules tab already applies (report_schedule_view.go): the creator, or a
// Super Admin, and nobody else - for list, open, edit and delete alike.
var creatorOwnedDoctypes = map[string]bool{"ScheduledReport": true, "DashboardDigest": true}

// CreatorOwnedDoctype reports whether doctype's records are their creator's.
func CreatorOwnedDoctype(doctype string) bool { return creatorOwnedDoctypes[doctype] }

// CreatorMayAccess reports whether this session may see or change one record
// of doctype that createdBy (documents.created_by, a user id) created.
func CreatorMayAccess(doctype, role, userID, createdBy string) bool {
	if !creatorOwnedDoctypes[doctype] || IsSuperAdmin(role) {
		return true
	}
	return userID != "" && createdBy == userID
}
