package engines

import "time"

// Stage 49.5.7: a till's own clock decides the day an offline sale posts on,
// so a till with a wrong or tampered clock could date sales into any open
// period. SettingPOSOfflineMaxBackdateDays (off) is the longest the
// till's day may lie behind the validation day; older sales post on the
// validation day (as a sale in a closed period does).
const SettingPOSOfflineMaxBackdateDays = "pos.offline_max_backdate_days"

func offlineSaleTooOld(schema, tillDay, today string) bool {
	limit := GetSettingIntForSchema(schema, SettingPOSOfflineMaxBackdateDays)
	if limit <= 0 {
		return false
	}
	t, err1 := time.Parse("2006-01-02", tillDay)
	n, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return false
	}
	return n.Sub(t) > time.Duration(limit)*24*time.Hour
}
