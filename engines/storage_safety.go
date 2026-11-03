package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"log"
	"sync"
	"time"
)

// Stage 49.13.3: query / storage safety.
//
// (a) The database statement timeout: platform.db_statement_timeout_seconds
// (0 = off, the default) is applied by db.SetSearchPath, the one place every
// tenant transaction passes, as SET LOCAL. Reports keep their own, longer
// bound (reportRunHardTimeout) - runReportRaced exempts its goroutine.
//
// (b) Disk and WAL thresholds: a watcher pages DISK_LOW / WAL_LARGE once a day
// while free disk is under platform.disk_free_alert_percent or the WAL is over
// platform.wal_size_alert_gb. Both read from the default client's settings.

func init() {
	db.StatementTimeoutFor = func(schema string) time.Duration {
		return time.Duration(GetSettingIntForSchema(schema, "platform.db_statement_timeout_seconds")) * time.Second
	}
}

// Probes and the alert sender are variables so a test can drive them.
var (
	storageDiskProbe = diskFreeAndTotal // free, total bytes of the server's disk
	storageWALProbe  = walDirSize       // bytes in pg_wal; error where it cannot be read
	storageSendAlert = SendOpsAlert
)

var storageWatch struct {
	sync.Mutex
	alerted map[string]time.Time
}

func resetStorageAlerts() {
	storageWatch.Lock()
	storageWatch.alerted = nil
	storageWatch.Unlock()
}

// walDirSize totals the WAL directory. pg_ls_waldir needs pg_monitor (or a
// superuser); where the server's role lacks it the check is skipped.
func walDirSize() (int64, error) {
	var n int64
	err := db.DB.QueryRow(`SELECT COALESCE(SUM(size), 0)::bigint FROM pg_ls_waldir()`).Scan(&n)
	return n, err
}

// storageOncePerDay reports whether kind has not been alerted in the last day.
func storageOncePerDay(kind string, now time.Time) bool {
	storageWatch.Lock()
	defer storageWatch.Unlock()
	if storageWatch.alerted == nil {
		storageWatch.alerted = map[string]time.Time{}
	}
	if last, ok := storageWatch.alerted[kind]; ok && now.Sub(last) < 24*time.Hour {
		return false
	}
	storageWatch.alerted[kind] = now
	return true
}

// checkStorageThresholds pages for each crossed threshold, once a day each.
func checkStorageThresholds(now time.Time) {
	if pct := GetSettingInt("default", "platform.disk_free_alert_percent"); pct > 0 {
		if free, total, err := storageDiskProbe(); err == nil && total > 0 {
			freePct := float64(free) * 100 / float64(total)
			if freePct < float64(pct) && storageOncePerDay("DISK_LOW", now) {
				msg := fmt.Sprintf("the server's disk has %.1f%% free (%.1f of %.1f GB), under the %d%% limit - clear space before backups and uploads start failing",
					freePct, float64(free)/(1<<30), float64(total)/(1<<30), pct)
				log.Printf("[STORAGE-WATCH] %s", msg)
				storageSendAlert("DISK_LOW", "storage-watch", msg)
			}
		}
	}
	if gb := GetSettingInt("default", "platform.wal_size_alert_gb"); gb > 0 {
		if size, err := storageWALProbe(); err == nil && size > int64(gb)<<30 && storageOncePerDay("WAL_LARGE", now) {
			msg := fmt.Sprintf("the database's WAL is %.1f GB, over the %d GB limit - check replication slots, archiving and long transactions", float64(size)/(1<<30), gb)
			log.Printf("[STORAGE-WATCH] %s", msg)
			storageSendAlert("WAL_LARGE", "storage-watch", msg)
		}
	}
}

// StartStorageWatch checks shortly after start and then every interval; only
// the scheduler leader pages.
func StartStorageWatch(ctx context.Context, interval time.Duration) {
	go func() {
		first := time.NewTimer(2 * time.Minute)
		defer first.Stop()
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if db.DB != nil && IsSchedulerLeader() {
				checkStorageThresholds(time.Now())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
