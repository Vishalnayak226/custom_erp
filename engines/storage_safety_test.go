package engines

import (
	"context"
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

func stage49133Sleep(secs string) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, "tenant_default"); err != nil {
		return err
	}
	_, err = tx.Exec(`SELECT pg_sleep(` + secs + `)`)
	return err
}

// Stage 49.13.3a: the database statement timeout cancels a statement past it
// when on, does nothing when off, and a report keeps its own longer bound.
func TestStage49133StatementTimeout(t *testing.T) {
	db.InitDB(testConnStr())
	const key = "platform.db_statement_timeout_seconds"
	defer SetSetting("default", key, "0", "test")

	if v := GetSettingInt("default", key); v != 0 {
		_ = SetSetting("default", key, "0", "test")
	}
	if err := stage49133Sleep("1.3"); err != nil {
		t.Fatalf("timeout off: a 1.3 s statement failed: %v", err)
	}
	if err := SetSetting("default", key, "1", "test"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := stage49133Sleep("30")
	if err == nil || !strings.Contains(err.Error(), "statement timeout") {
		t.Fatalf("timeout on: a 30 s statement ended with %v, want a statement timeout", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("cancelled after %v, want well under 30 s", time.Since(start))
	}
	// A report keeps its own, longer bound.
	rows, err := runReportRaced(context.Background(), ReportDefinition{ID: "stage49133-report", Run: func(string, map[string]string) ([]map[string]interface{}, error) {
		return nil, stage49133Sleep("1.5")
	}}, "default", nil)
	if err != nil || rows != nil {
		t.Fatalf("a report under the statement timeout: %v", err)
	}
}

// Stage 49.13.3b: the alert fires over the disk and WAL thresholds, once a
// day, and stays quiet under them or when a threshold is off.
func TestStage49133StorageAlerts(t *testing.T) {
	db.InitDB(testConnStr())
	defer SetSetting("default", "platform.disk_free_alert_percent", "0", "test")
	defer SetSetting("default", "platform.wal_size_alert_gb", "0", "test")
	oldDisk, oldWAL, oldSend := storageDiskProbe, storageWALProbe, storageSendAlert
	defer func() { storageDiskProbe, storageWALProbe, storageSendAlert = oldDisk, oldWAL, oldSend }()
	var sent []string
	storageSendAlert = func(sev, src, msg string) { sent = append(sent, sev+"|"+msg) }
	storageDiskProbe = func() (uint64, uint64, error) { return 5 << 30, 100 << 30, nil } // 5% free
	storageWALProbe = func() (int64, error) { return 9 << 30, nil }
	_ = SetSetting("default", "platform.disk_free_alert_percent", "10", "test")
	_ = SetSetting("default", "platform.wal_size_alert_gb", "8", "test")

	now := time.Now()
	checkStorageThresholds(now)
	if len(sent) != 2 || !strings.HasPrefix(sent[0], "DISK_LOW|") || !strings.HasPrefix(sent[1], "WAL_LARGE|") {
		t.Fatalf("over both thresholds: %v", sent)
	}
	checkStorageThresholds(now.Add(time.Hour))
	if len(sent) != 2 {
		t.Fatalf("repeated within a day: %v", sent)
	}
	checkStorageThresholds(now.Add(25 * time.Hour))
	if len(sent) != 4 {
		t.Fatalf("after a day: %v", sent)
	}

	sent = nil
	resetStorageAlerts()
	storageDiskProbe = func() (uint64, uint64, error) { return 50 << 30, 100 << 30, nil }
	storageWALProbe = func() (int64, error) { return 1 << 30, nil }
	checkStorageThresholds(now)
	if len(sent) != 0 {
		t.Fatalf("under the thresholds: %v", sent)
	}
	storageDiskProbe = func() (uint64, uint64, error) { return 1 << 30, 100 << 30, nil }
	_ = SetSetting("default", "platform.disk_free_alert_percent", "0", "test")
	checkStorageThresholds(now)
	if len(sent) != 0 {
		t.Fatalf("threshold off: %v", sent)
	}
}
