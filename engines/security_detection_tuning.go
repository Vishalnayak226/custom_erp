package engines

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 49.5.4 (tuning): the abuse detections use one fixed threshold. Two
// settings, both off, let a client move it:
//
//   - security.detection_volume_docs_per_day: the daily document volume at
//     which the burst thresholds double (scales linearly, capped at 5x). A busy
//     client's normal day then does not page; a quiet one keeps today's numbers.
//   - security.detection_role_scale: "Role=percent" pairs; an actor holding the
//     role is held to that percent of the threshold (200 = twice as many before
//     it fires, 50 = half as many).
//
// With both empty the limit is the rule's own Threshold, as before.
const (
	SettingDetectionVolumeDocsPerDay = "security.detection_volume_docs_per_day"
	SettingDetectionRoleScale        = "security.detection_role_scale"

	detectionVolumeDays     = 14
	detectionVolumeMaxScale = 5.0
)

// volumeTunedRules are the count-per-window bursts whose normal level follows
// how busy the client is. Credential, audit-evidence and one-event rules never
// scale.
var volumeTunedRules = map[string]bool{
	"refund_burst": true, "manual_discount_burst": true, "offline_sale_burst": true,
	"stock_adjustment_burst": true, "negative_stock_burst": true, "till_reversal_burst": true,
	"access_denied_burst": true, "export_burst": true,
}

type detectionTuning struct {
	schema      string
	docsPerDay  int
	roleScale   map[string]float64 // lower-cased role -> percent / 100
	volume      float64
	volumeKnown bool
	roleOf      map[string]string
}

func loadDetectionTuning(schema string, now time.Time) *detectionTuning {
	return &detectionTuning{
		schema:     schema,
		docsPerDay: GetSettingIntForSchema(schema, SettingDetectionVolumeDocsPerDay),
		roleScale:  parseDetectionRoleScale(GetSettingStringForSchema(schema, SettingDetectionRoleScale)),
		roleOf:     map[string]string{},
	}
}

// parseDetectionRoleScale reads "Store Manager=200, Cashier=50" (commas or
// new lines). A pair that is not Role=number between 10 and 1000 is ignored.
func parseDetectionRoleScale(s string) map[string]float64 {
	out := map[string]float64{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(kv[1]), "%")), 64)
		role := strings.ToLower(strings.TrimSpace(kv[0]))
		if err != nil || role == "" || pct < 10 || pct > 1000 {
			continue
		}
		out[role] = pct / 100
	}
	return out
}

// volumeScale is the client's 14-day daily document average over the
// configured level, never below 1 and never above 5. Read once per sweep.
func (t *detectionTuning) volumeScale(now time.Time) float64 {
	if t.docsPerDay <= 0 {
		return 1
	}
	if t.volumeKnown {
		return t.volume
	}
	t.volumeKnown, t.volume = true, 1
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE created_at > $1`, t.schema),
		now.Add(-detectionVolumeDays*24*time.Hour)).Scan(&n); err != nil {
		return 1
	}
	perDay := float64(n) / detectionVolumeDays
	t.volume = math.Min(detectionVolumeMaxScale, math.Max(1, perDay/float64(t.docsPerDay)))
	return t.volume
}

func (t *detectionTuning) roleFor(subject string) string {
	if len(t.roleScale) == 0 || subject == "" {
		return ""
	}
	if r, ok := t.roleOf[subject]; ok {
		return r
	}
	var role sql.NullString
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT role FROM %s.users WHERE id = $1 OR LOWER(username) = LOWER($1) LIMIT 1`, t.schema), subject).Scan(&role)
	t.roleOf[subject] = strings.ToLower(role.String)
	return t.roleOf[subject]
}

// limit is the count at which rule fires for subject. Never below 1.
func (t *detectionTuning) limit(rule SecurityDetectionRule, subject string, now time.Time) int {
	scale := 1.0
	if volumeTunedRules[rule.Key] {
		scale = t.volumeScale(now)
	}
	if role := t.roleFor(subject); role != "" {
		if s, ok := t.roleScale[role]; ok {
			scale *= s
		}
	}
	if scale == 1 {
		return rule.Threshold
	}
	n := int(math.Ceil(float64(rule.Threshold) * scale))
	if n < 1 {
		n = 1
	}
	return n
}
