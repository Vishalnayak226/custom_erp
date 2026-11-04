package engines

import (
	"bufio"
	"bytes"
	"custom_erp/db"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"

	"github.com/lib/pq"
)

// Stage 146.5.6: a feed = template + product group + "ready only" + format +
// destination. Every run is stored with its skipped items and reasons; an
// email destination goes through the real SMTP sender (here a local listener,
// never a real mail server); the outbox sends its email events for real when
// SMTP is set up and says "not sent" when it is not.

type s14656SMTP struct {
	mu   sync.Mutex
	msgs [][]byte
	rcpt []string
	port string
	ln   net.Listener
}

func s14656StartSMTP(t *testing.T) *s14656SMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &s14656SMTP{ln: ln}
	_, s.port, _ = net.SplitHostPort(ln.Addr().String())
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := func(l string) { _, _ = c.Write([]byte(l + "\r\n")) }
				w("220 fake ESMTP")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						w("250 fake")
					case strings.HasPrefix(cmd, "MAIL"):
						w("250 ok")
					case strings.HasPrefix(cmd, "RCPT"):
						s.mu.Lock()
						s.rcpt = append(s.rcpt, strings.TrimSpace(line))
						s.mu.Unlock()
						w("250 ok")
					case strings.HasPrefix(cmd, "DATA"):
						w("354 go")
						var buf bytes.Buffer
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							buf.WriteString(l)
						}
						s.mu.Lock()
						s.msgs = append(s.msgs, buf.Bytes())
						s.mu.Unlock()
						w("250 queued")
					case strings.HasPrefix(cmd, "QUIT"):
						w("221 bye")
						return
					default:
						w("250 ok")
					}
				}
			}(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", s.port)
	t.Setenv("SMTP_FROM", "feeds@example.test")
	t.Setenv("SMTP_USER", "")
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	return s
}

func (s *s14656SMTP) messages() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.msgs...)
}

// attachment returns the first attachment's name and decoded bytes.
func s14656Attachment(t *testing.T, raw []byte) (string, string) {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("message does not parse: %v", err)
	}
	_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		t.Fatalf("not multipart: %q", m.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FileName() == "" {
			continue
		}
		body, _ := io.ReadAll(p)
		dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(body)), ""))
		if err != nil {
			t.Fatalf("attachment is not base64: %v", err)
		}
		return p.FileName(), string(dec)
	}
	t.Fatal("no attachment in the message")
	return "", ""
}

func s14656Group(t *testing.T, f s14652Fixture, id string, items ...string) {
	t.Helper()
	members := make([]map[string]string, 0, len(items))
	for _, it := range items {
		members = append(members, map[string]string{"item_code": it})
	}
	pimInsertDoc(t, f.schema, id, "PIMProductGroup", "Active", map[string]interface{}{
		"code": id, "name": id, "group_type": "Static", "members": members})
}

func s14656Feed(t *testing.T, f s14652Fixture, id string, extra map[string]interface{}) {
	t.Helper()
	d := map[string]interface{}{"code": id, "name": id, "export_template": f.exportID, "ready_only": "No",
		"format": "csv", "destination": "Download", "status": "Active"}
	for k, v := range extra {
		d[k] = v
	}
	pimInsertDoc(t, f.schema, id, "PIMFeed", "Active", d)
}

func s14656Cleanup(f s14652Fixture) {
	_, _ = db.DB.Exec("DELETE FROM " + f.schema + ".documents WHERE id LIKE 'S14656%' OR doctype = 'PIMExportRun' AND data->>'feed' LIKE 'S14656%'")
}

func s14656Run(t *testing.T, f s14652Fixture, runID string) map[string]interface{} {
	t.Helper()
	var raw string
	if err := db.DB.QueryRow("SELECT data FROM "+f.schema+".documents WHERE id = $1 AND doctype = 'PIMExportRun'", runID).Scan(&raw); err != nil {
		t.Fatalf("run %q is not recorded: %v", runID, err)
	}
	var m map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

func TestStage14656FeedIsFilteredByGroupAndRecorded(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	s14652Item(t, f, "S14656-A1", "NECK", map[string]interface{}{"title_src": "Chain one", "colour_src": "Red"})
	s14652Item(t, f, "S14656-A2", "NECK", map[string]interface{}{"title_src": "Chain two", "colour_src": "Purple"})
	s14652Item(t, f, "S14656-A3", "NECK", map[string]interface{}{"title_src": "Chain three", "colour_src": "Blue"})
	s14656Group(t, f, "S14656-G", "S14656-A1", "S14656-A2")
	s14656Feed(t, f, "S14656-F", map[string]interface{}{"product_group": "S14656-G"})

	res, err := RunPIMFeed("default", "system", "S14656-F")
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 1 || !strings.Contains(string(res.Data), "S14656-A1") ||
		strings.Contains(string(res.Data), "S14656-A3") || strings.Contains(string(res.Data), "S14656-A2") {
		t.Fatalf("feed rows = %d, file:\n%s", res.Rows, res.Data)
	}
	run := s14656Run(t, f, res.RunID)
	if run["status"] != "Exported with skips" || run["feed"] != "S14656-F" || run["rows"] != float64(1) || run["skipped_count"] != float64(1) {
		t.Errorf("run record = %+v", run)
	}
	skipped, _ := run["skipped"].([]interface{})
	if len(skipped) != 1 {
		t.Fatalf("skipped list = %+v", run["skipped"])
	}
	one, _ := skipped[0].(map[string]interface{})
	if one["item"] != "S14656-A2" || one["column"] != "Colour" || !strings.Contains(strings.ToLower(one["reason"].(string)), "purple") {
		t.Errorf("skipped item = %+v", one)
	}
	if run["delivery_status"] != "Download" {
		t.Errorf("delivery = %v, want Download", run["delivery_status"])
	}
}

func TestStage14656ReadyOnlySkipsWhatIsNotReady(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	s14652Item(t, f, "S14656-R1", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Red"})
	ready, err := CheckPublishReadiness("default", "S14656-R1", f.channel)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Ready {
		t.Skip("fixture item is ready; this test needs one that is not")
	}
	s14656Feed(t, f, "S14656-FR", map[string]interface{}{"ready_only": "Yes"})
	res, err := RunPIMFeed("default", "system", "S14656-FR")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.Data), "S14656-R1") {
		t.Errorf("an item that is not ready was exported:\n%s", res.Data)
	}
	var found *PIMExportSkip
	for i, s := range res.Skipped {
		if s.Item == "S14656-R1" {
			found = &res.Skipped[i]
		}
	}
	if found == nil || !strings.Contains(found.Reason, "Not ready") || !strings.Contains(found.Reason, ready.MissingFields[0]) {
		t.Errorf("skip = %+v, want 'Not ready' with the missing fields %v", found, ready.MissingFields)
	}
}

func TestStage14656FeedEmailGoesThroughRealSender(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	smtpSrv := s14656StartSMTP(t)
	s14652Item(t, f, "S14656-E1", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Red"})
	s14656Group(t, f, "S14656-GE", "S14656-E1")
	s14656Feed(t, f, "S14656-FE", map[string]interface{}{"product_group": "S14656-GE", "destination": "Email", "recipient_email": "buyer@example.test"})

	res, err := RunPIMFeed("default", "system", "S14656-FE")
	if err != nil {
		t.Fatal(err)
	}
	run := s14656Run(t, f, res.RunID)
	if run["delivery_status"] != "Sent" {
		t.Fatalf("delivery = %v (%v), want Sent", run["delivery_status"], run["delivery_message"])
	}
	msgs := smtpSrv.messages()
	if len(msgs) != 1 {
		t.Fatalf("mail server got %d messages, want 1", len(msgs))
	}
	if !strings.Contains(strings.Join(smtpSrv.rcpt, " "), "buyer@example.test") {
		t.Errorf("recipients = %v", smtpSrv.rcpt)
	}
	name, body := s14656Attachment(t, msgs[0])
	if !strings.HasSuffix(name, ".csv") || !strings.Contains(body, "S14656-E1") {
		t.Errorf("attachment %q = %q", name, body)
	}
}

func TestStage14656EmailNotSetUpIsShownNotHidden(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	t.Setenv("SMTP_HOST", "")
	s14652Item(t, f, "S14656-N1", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Red"})
	s14656Group(t, f, "S14656-GN", "S14656-N1")
	s14656Feed(t, f, "S14656-FN", map[string]interface{}{"product_group": "S14656-GN", "destination": "Email", "recipient_email": "buyer@example.test"})
	res, err := RunPIMFeed("default", "system", "S14656-FN")
	if err != nil {
		t.Fatal(err)
	}
	run := s14656Run(t, f, res.RunID)
	if ds, _ := run["delivery_status"].(string); !strings.HasPrefix(ds, "Not sent") {
		t.Errorf("delivery = %q, want it to say Not sent", ds)
	}
}

func TestStage14656OutboxSendsEmailEventsForReal(t *testing.T) {
	schema, _, cleanup := pimTestFixture(t, "S14656O")
	defer cleanup()
	smtpSrv := s14656StartSMTP(t)
	publish := func(name string, payload map[string]interface{}) string {
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_ = db.SetSearchPath(tx, schema)
		if err := PublishEvent(tx, schema, name, payload); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := db.DB.QueryRow("SELECT id::text FROM "+schema+".integration_event_outbox WHERE event_name = $1 ORDER BY created_at DESC LIMIT 1", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id1 := publish("email.send", map[string]interface{}{"to": "a@example.test", "subject": "Hello", "body": "Body text",
		"attach_name": "x.csv", "attach_type": "text/csv", "attach_b64": base64.StdEncoding.EncodeToString([]byte("a,b\n1,2\n"))})
	id2 := publish("pim.export.scheduled_delivery", map[string]interface{}{"schedule_id": "S", "export_template": "T",
		"recipient_email": "b@example.test", "csv": "item,title\nS14656O-1,Chain\n"})
	// One pass takes the 10 oldest events and the schema is shared, so other
	// tests' events can be ahead of these: drain until each of ours is logged.
	drain := func(ids ...string) {
		for i := 0; i < 100; i++ {
			var n int
			_ = db.DB.QueryRow("SELECT count(DISTINCT event_id) FROM "+schema+".integration_event_log WHERE event_id::text = ANY($1)", pq.Array(ids)).Scan(&n)
			if n == len(ids) {
				return
			}
			processOutbox(schema)
		}
	}
	drain(id1, id2)
	if got := strings.Join(smtpSrv.rcpt, " "); !strings.Contains(got, "a@example.test") || !strings.Contains(got, "b@example.test") {
		t.Fatalf("mail server recipients = %v, want a@ and b@example.test", smtpSrv.rcpt)
	}
	for _, id := range []string{id1, id2} {
		var ch, st string
		if err := db.DB.QueryRow("SELECT channel, status FROM "+schema+".integration_event_log WHERE event_id = $1::uuid", id).Scan(&ch, &st); err != nil {
			t.Fatalf("no log row for %s: %v", id, err)
		}
		if ch != "Email" || st != "Sent" {
			t.Errorf("event log = %s / %s, want Email / Sent", ch, st)
		}
	}

	// not set up: the event is still logged, and the log says so
	t.Setenv("SMTP_HOST", "")
	id3 := publish("email.send", map[string]interface{}{"to": "c@example.test", "subject": "Hi", "body": "x"})
	drain(id3)
	var st string
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".integration_event_log WHERE event_id = $1::uuid", id3).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st, "Not sent") {
		t.Errorf("without SMTP the log status = %q, want Not sent", st)
	}
}

// The export freeze is enforced on requests, so a schedule has to honour it
// itself: nothing leaves, nothing piles up, and the file goes again on unfreeze.
func TestStage14656ExportFreezeStopsScheduledFeed(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	smtpSrv := s14656StartSMTP(t)
	s14652Item(t, f, "S14656-Z1", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Red"})
	s14656Group(t, f, "S14656-GZ", "S14656-Z1")
	s14656Feed(t, f, "S14656-FZ", map[string]interface{}{"product_group": "S14656-GZ", "destination": "Email", "recipient_email": "buyer@example.test"})
	sched := map[string]interface{}{"feed": "S14656-FZ", "frequency": "Daily", "next_run_date": "2026-01-01"}
	pimInsertDoc(t, f.schema, "S14656-SCZ", "PIMExportSchedule", "Active", sched)
	if err := SetSetting("default", SettingFreezeExports, "true", "test"); err != nil {
		t.Fatal(err)
	}
	defer SetSetting("default", SettingFreezeExports, "false", "test")

	runs := func() (n int) {
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".documents WHERE doctype = 'PIMExportRun' AND data->>'feed' = 'S14656-FZ'").Scan(&n)
		return n
	}
	runPIMExportSchedule("default", f.schema, "S14656-SCZ", sched)
	if got := len(smtpSrv.messages()); got != 0 {
		t.Fatalf("mail server got %d messages under the freeze, want 0", got)
	}
	if runs() != 0 {
		t.Errorf("feed runs recorded under the freeze = %d, want 0", runs())
	}
	if st, _ := sched["last_run_status"].(string); !strings.HasPrefix(st, "Refused") {
		t.Errorf("last_run_status = %q, want Refused", st)
	}
	if sched["next_run_date"] == "2026-01-01" {
		t.Error("next_run_date did not move on, so the schedule would refuse again on every tick")
	}

	// a file queued before the freeze stays too; other outbox email still goes
	if isEmail, st, _ := dispatchOutboxEmailEvent("default", "pim.export.scheduled_delivery", map[string]interface{}{
		"export_template": "T", "recipient_email": "b@example.test", "csv": "a,b\n"}); !isEmail || st != EmailNotSent {
		t.Errorf("queued file under the freeze = %v / %q, want Not sent", isEmail, st)
	}
	if _, st, _ := dispatchOutboxEmailEvent("default", "email.send", map[string]interface{}{
		"to": "c@example.test", "subject": "PO", "body": "x"}); st != EmailSent {
		t.Errorf("ordinary email under the freeze = %q, want Sent", st)
	}
	if got := len(smtpSrv.messages()); got != 1 {
		t.Fatalf("mail server got %d messages, want only the ordinary email", got)
	}

	if err := SetSetting("default", SettingFreezeExports, "false", "test"); err != nil {
		t.Fatal(err)
	}
	runPIMExportSchedule("default", f.schema, "S14656-SCZ", sched)
	if got := len(smtpSrv.messages()); got != 2 {
		t.Errorf("mail server got %d messages after unfreezing, want 2", got)
	}
	if runs() != 1 {
		t.Errorf("feed runs after unfreezing = %d, want 1", runs())
	}
}

func TestStage14656ScheduleCanRunAFeed(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	s14652Item(t, f, "S14656-S1", "NECK", map[string]interface{}{"title_src": "Chain", "colour_src": "Red"})
	s14656Group(t, f, "S14656-GS", "S14656-S1")
	s14656Feed(t, f, "S14656-FS", map[string]interface{}{"product_group": "S14656-GS"})
	// a schedule that names a feed needs no template and no recipient of its own
	sched := map[string]interface{}{"feed": "S14656-FS", "frequency": "Daily", "next_run_date": "2026-01-01"}
	if err := ValidatePIMExportScheduleDocument("default", sched); err != nil {
		t.Fatalf("a schedule with a feed was refused: %v", err)
	}
	if err := ValidatePIMExportScheduleDocument("default", map[string]interface{}{"feed": "NOPE", "frequency": "Daily", "next_run_date": "2026-01-01"}); err == nil {
		t.Error("a schedule naming a feed that does not exist was accepted")
	}
	pimInsertDoc(t, f.schema, "S14656-SCH", "PIMExportSchedule", "Active", sched)
	runPIMExportSchedule("default", f.schema, "S14656-SCH", sched)
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".documents WHERE doctype = 'PIMExportRun' AND data->>'feed' = 'S14656-FS'").Scan(&n)
	if n != 1 {
		t.Errorf("scheduled feed runs recorded = %d, want 1", n)
	}
	if sched["last_run_status"] == nil || strings.HasPrefix(sched["last_run_status"].(string), "Failed") {
		t.Errorf("last_run_status = %v", sched["last_run_status"])
	}
}

func TestStage14656FeedValidation(t *testing.T) {
	f, cleanup := s14652Setup(t)
	defer cleanup()
	defer s14656Cleanup(f)
	base := func() map[string]interface{} {
		return map[string]interface{}{"code": "X", "export_template": f.exportID, "ready_only": "No", "format": "csv", "destination": "Download", "status": "Active"}
	}
	if err := ValidatePIMFeedDocument("default", base()); err != nil {
		t.Fatalf("a good feed was refused: %v", err)
	}
	bad := base()
	bad["destination"], bad["recipient_email"] = "Email", "not-an-address"
	if err := ValidatePIMFeedDocument("default", bad); err == nil {
		t.Error("an email feed with a bad address was accepted")
	}
	bad = base()
	bad["product_group"] = "NO-SUCH-GROUP"
	if err := ValidatePIMFeedDocument("default", bad); err == nil {
		t.Error("a feed naming a missing product group was accepted")
	}
	bad = base()
	bad["format"] = "pdf"
	if err := ValidatePIMFeedDocument("default", bad); err == nil {
		t.Error("an unknown format was accepted")
	}
}
