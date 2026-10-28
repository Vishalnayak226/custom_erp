package securityscan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Stage 49.8.7's egress inventory: every place the code can open a connection
// or send a request off this machine, the class of destination it reaches,
// what keeps a client-typed address from reaching inside the network, and what
// stops it in an emergency.
//
// The prose is the deliverable, as in bypass_test.go. The test keeps it true:
//
//   - a file that gains an outbound construct not listed here fails, with the
//     file and the construct named - it needs an entry before it ships;
//   - a listed count that no longer matches fails, so one reviewed call cannot
//     quietly cover a second one added beside it;
//   - every entry names Evidence, text the file must contain for its Guard and
//     KillSwitch lines to be true (the guarded client, the switch check), so
//     an entry cannot outlive the control it describes.
//
// Destination classes (49.8.7's list): channel platforms and couriers go only
// through engines/connector_http.go; payment has no outbound call at all (Pine
// Labs is recorded and reconciled locally; the terminal talks to Pine Labs
// itself); printers are reached by the browser through PrintBridge on the
// shop's own computer, never by the server. Neither of the last two has an
// entry because neither opens a connection from this code.
var egressKinds = map[string]*regexp.Regexp{
	"http-client":    regexp.MustCompile(`\bhttp\.Client\{`),
	"http-transport": regexp.MustCompile(`\bhttp\.Transport\{`),
	"default-client": regexp.MustCompile(`\bhttp\.Default(Client|Transport)\b`),
	"http-request":   regexp.MustCompile(`\bhttp\.NewRequest(WithContext)?\(`),
	"http-shorthand": regexp.MustCompile(`\bhttp\.(Get|Post|PostForm|Head)\(`),
	"dial":           regexp.MustCompile(`\bnet\.Dial(Timeout)?\(|\bnet\.Dialer\{|\btls\.Dial(WithDialer)?\(`),
	"smtp":           regexp.MustCompile(`\bsmtp\.(SendMail|Dial|NewClient)\(`),
	"dns":            regexp.MustCompile(`\bnet\.(Lookup[A-Za-z]+\(|DefaultResolver\b)`),
}

type egressEntry struct {
	Uses        map[string]int
	Destination string
	Guard       string
	KillSwitch  string
	Evidence    []string
}

const (
	viaGuard     = "newOutboundClient(" // engines/outbound_guard.go's client
	viaKillCheck = "ExternalSideEffectsEnabled()"
	// 49.8.7a: the emergency switch, the client's own Outside calls setting
	// and the environment, in one answer (engines/outside_calls.go).
	viaOutsideCalls = "OutsideCallsAllowed"
	viaCallGate     = "outsideCallRefusal("
)

var reviewedEgress = map[string]egressEntry{
	"engines/outbound_guard.go": {
		Uses:        map[string]int{"http-client": 1, "http-transport": 1, "dial": 1, "dns": 1},
		Destination: `None of its own: the guard every client-typed address goes through.`,
		Guard: `Is the guard (134.2): every resolved address must be public, the dial goes to the address
			that was checked (no DNS rebinding), no proxy from the environment, at most three redirects
			and never https to http, response headers and bodies capped.`,
		KillSwitch: `Not a call site; each caller below carries its own switch.`,
		Evidence:   []string{"publicAddress(", "LookupIPAddr("},
	},
	"engines/connector_http.go": {
		Uses: map[string]int{"http-request": 1},
		Destination: `Channel platforms (Shopify, BigCommerce, Magento, Amazon, Flipkart, partner channels) and
			couriers (Delhivery, Shiprocket). Every connector and courier adapter reaches the network only
			through doConnectorRequest, so this one line covers all of them.`,
		Guard: `The outbound guard: a shop's address is client-typed. Per-platform circuit breaker, a hard
			timeout over the client's own, a 32 MB answer cap.`,
		KillSwitch: `outsideCallRefusal (49.8.7a), before the call leaves, as CONN-0225 - the code a publish
			job already stays Queued on - so pending work goes out once the switch is back on.
			ERP_DISABLE_EXTERNAL_SIDE_EFFECTS and the client's Outside calls setting refuse every write
			(anything but GET/HEAD), a local one too; reads still run so orders and tracking can be
			reconciled. Outside production (unless ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1) every call to
			another machine is refused, reads too; loopback test doubles and local sandboxes still go.
			The client comes from the context: the API middleware, the job runner and the channel sync,
			PIM publish and return-pickup paths name it.`,
		Evidence: []string{viaGuard, viaCallGate},
	},
	"engines/webhook.go": {
		Uses:        map[string]int{"http-request": 1, "dns": 1},
		Destination: `Client-registered webhook subscriptions (signed event deliveries).`,
		Guard: `The outbound guard, no redirects followed; the address is also resolved and refused at
			registration (resolvesToPublicAddressOnly, the LookupIP here).`,
		KillSwitch: `Off in a sandbox client, for a client whose Outside calls are off, outside production
			and under the emergency switch: the delivery is simulated, and the job's result says why.`,
		Evidence: []string{viaGuard, viaOutsideCalls},
	},
	"engines/extensions.go": {
		Uses:        map[string]int{"http-request": 1},
		Destination: `Client-registered extension hooks: a signed POST to the hook's URL at a document event.`,
		Guard:       `The outbound guard, no redirects followed, the hook's own timeout.`,
		KillSwitch: `outsideCallRefusal (49.8.7a), the connectors' gate: not called under the emergency
			switch or when the client's Outside calls are off (a local hook too), nor outside production
			unless the hook is on loopback (the extension SDK on a developer's machine). A before_save hook
			that is not called stops the save (EXT-0289, the reason named) - a check that did not run must
			not let it through, the timeout rule; an after_save one logs the refusal on its call log.`,
		Evidence: []string{viaGuard, viaCallGate},
	},
	"engines/notifications.go": {
		Uses:        map[string]int{"http-request": 1},
		Destination: `A client's notification webhook (alerts a client routes to its own chat or system).`,
		Guard:       `The outbound guard.`,
		KillSwitch:  `Off in a sandbox client, when the client's Outside calls are off, outside production and under the emergency switch.`,
		Evidence:    []string{viaGuard, viaOutsideCalls},
	},
	"engines/web_push.go": {
		Uses:        map[string]int{"http-request": 1},
		Destination: `Browser push services (the endpoint a subscribing browser handed over).`,
		Guard:       `The outbound guard: the endpoint came from a browser.`,
		KillSwitch:  `Off in a sandbox client, when the client's Outside calls are off, outside production and under the emergency switch.`,
		Evidence:    []string{viaGuard, viaOutsideCalls},
	},
	"engines/crm_providers.go": {
		Uses: map[string]int{"http-request": 6, "dial": 1, "smtp": 1},
		Destination: `A client's CRM senders: SMS and WhatsApp gateways, a CRM webhook, and the client's own
			mail server for CRM email.`,
		Guard: `HTTP through the outbound guard. The mail server dial goes through the guard's dial when
			the sender names its own host (client-typed, 49.8.7 - it was a plain dial before); a sender
			using the server's SMTP_HOST dials the operator's relay directly.`,
		KillSwitch: `Every send passes crm_send_gate.go, which simulates it in a sandbox client, when the
			client's Outside calls are off, outside production and under the emergency switch; the
			WhatsApp template sync here asks the same switch itself.`,
		Evidence: []string{viaGuard, "outboundDial", viaOutsideCalls},
	},
	"engines/image_drive.go": {
		Uses:        map[string]int{"http-client": 1, "http-transport": 1, "http-request": 1},
		Destination: `A client's image drive (S3-compatible storage) - product images read and written.`,
		Guard: `Its own client, every connection through guardedDial with the drive's own on-premise
			switch (IMAGE_DRIVE_ALLOW_PRIVATE); no proxy.`,
		KillSwitch: `Not switched: the drive is the client's own storage, not a message to a third party,
			and media reads and writes are part of saving a product.`,
		Evidence: []string{"guardedDial("},
	},
	"engines/alerting.go": {
		Uses:        map[string]int{"http-client": 1, "http-request": 1},
		Destination: `The operator's ops-alert webhook (OPS_ALERT_WEBHOOK_URL, set on the server).`,
		Guard: `Not client-typed: the address is the operator's own setting, so a plain client with a
			5-second timeout.`,
		KillSwitch: `Off outside production and under the emergency switch; the alert is logged instead.`,
		Evidence:   []string{"OPS_ALERT_WEBHOOK_URL", viaKillCheck},
	},
	"engines/password_reset.go": {
		Uses:        map[string]int{"smtp": 3},
		Destination: `The operator's mail relay (SMTP_HOST) - password reset and email-change mail.`,
		Guard:       `Not client-typed: the relay is the server's own setting.`,
		KillSwitch:  `Off outside production and under the emergency switch; the mail is logged as not sent. Not the client's Outside calls setting: account recovery is platform security mail (49.8.7a).`,
		Evidence:    []string{"SMTP_HOST", viaKillCheck},
	},
	"engines/document_mail.go": {
		Uses:        map[string]int{"smtp": 1},
		Destination: `The operator's mail relay (SMTP_HOST) - documents emailed to a customer or vendor.`,
		Guard:       `Not client-typed: the relay is the server's own setting.`,
		KillSwitch:  `Off in a sandbox client, when the client's Outside calls are off, outside production and under the emergency switch.`,
		Evidence:    []string{"SMTP_HOST", viaOutsideCalls},
	},
	"engines/dashboard_digest_html.go": {
		Uses:        map[string]int{"smtp": 1},
		Destination: `The operator's mail relay (SMTP_HOST) - scheduled dashboard digests.`,
		Guard:       `Not client-typed: the relay is the server's own setting.`,
		KillSwitch:  `Off in a sandbox client, when the client's Outside calls are off, outside production and under the emergency switch.`,
		Evidence:    []string{"SMTP_HOST", viaOutsideCalls},
	},
	"internal/server/setup_workbook.go": {
		Uses:        map[string]int{"http-request": 1},
		Destination: `None: the Setup Workbook builds a request and hands it to this server's own handler in process.`,
		Guard:       `Nothing is dialed - there is no client in this file.`,
		KillSwitch:  `Not an outbound call.`,
		Evidence:    []string{"swRecorder"},
	},
	"internal/server/handlers_pos_offline_sales.go": {
		Uses:        map[string]int{"http-request": 1},
		Destination: `None: Validate on POS > Offline Sales builds the held checkout request and hands it to this server's own handleCheckout in process (Stage 49.5.6a).`,
		Guard:       `Nothing is dialed - there is no client in this file.`,
		KillSwitch:  `Not an outbound call.`,
		Evidence:    []string{"swRecorder", "handleCheckout(rec, req)"},
	},
	"cmd/doclint/external.go": {
		Uses:        map[string]int{"http-client": 1, "http-transport": 1, "http-request": 1, "dial": 1, "dns": 1},
		Destination: `Public web pages the documentation links to (doclint's opt-in external check).`,
		Guard: `Its own: refuses a link that resolves to a non-public address, dials the address it checked,
			no proxy, at most four redirects, each re-checked.`,
		KillSwitch: `Not the server: a developer tool run by hand, reading pages only.`,
		Evidence:   []string{"publicIP(", "Proxy: nil"},
	},
	"cmd/edgecheck/main.go": {
		Uses:        map[string]int{"http-client": 2, "http-request": 4, "dial": 2},
		Destination: `The operator's own deployment, named with -base: its edge, TLS and firewalled ports.`,
		Guard:       `The operator names the target; it must be an https URL.`,
		KillSwitch:  `Not the server: an operator's check, run by hand.`,
		Evidence:    []string{`baseURL.Scheme != "https"`},
	},
}

// scanEgress counts every egress construct in the non-test Go files under root.
func scanEgress(root string) (map[string]map[string]int, error) {
	found := map[string]map[string]int{}
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if skippedDirs[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for kind, re := range egressKinds {
			if n := len(re.FindAllIndex(data, -1)); n > 0 {
				if found[rel] == nil {
					found[rel] = map[string]int{}
				}
				found[rel][kind] = n
			}
		}
		return nil
	})
	return found, err
}

func TestEveryEgressPointIsReviewed(t *testing.T) {
	found, err := scanEgress(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for file, uses := range found {
		entry, ok := reviewedEgress[file]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s opens outbound connections (%v) with no reviewed entry: add one to reviewedEgress naming its destination, guard and kill switch", file, uses))
			continue
		}
		for kind, n := range uses {
			if entry.Uses[kind] != n {
				problems = append(problems, fmt.Sprintf("%s: %d %s, the review lists %d - review the new call and update the entry", file, n, kind, entry.Uses[kind]))
			}
		}
		for kind, n := range entry.Uses {
			if uses[kind] == 0 {
				problems = append(problems, fmt.Sprintf("%s: the review lists %d %s that are gone - update the entry", file, n, kind))
			}
		}
	}
	for file, entry := range reviewedEgress {
		if _, ok := found[file]; !ok {
			problems = append(problems, fmt.Sprintf("%s: reviewed but has no outbound construct any more - remove the entry", file))
			continue
		}
		if strings.TrimSpace(entry.Destination) == "" || strings.TrimSpace(entry.Guard) == "" || strings.TrimSpace(entry.KillSwitch) == "" || len(entry.Evidence) == 0 {
			problems = append(problems, file+": an entry needs a destination, a guard, a kill switch line and evidence")
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range entry.Evidence {
			if !strings.Contains(string(data), ev) {
				problems = append(problems, fmt.Sprintf("%s: the entry rests on %q, which the file no longer contains", file, ev))
			}
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// A client-typed address must not get its own client: in the server's code,
// only the guard, the image drive (whose client dials through the guard) and
// the operator's ops-alert webhook build an http.Client or Transport, and
// nothing uses the default client, which has no timeout and honours a proxy
// from the environment.
func TestOnlyTheGuardBuildsAnHTTPClient(t *testing.T) {
	found, err := scanEgress(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"engines/outbound_guard.go": true, "engines/image_drive.go": true, "engines/alerting.go": true}
	for file, uses := range found {
		if strings.HasPrefix(file, "cmd/") {
			continue // developer and operator tools, not the server
		}
		if uses["default-client"] > 0 || uses["http-shorthand"] > 0 {
			t.Errorf("%s uses Go's default HTTP client (no timeout, environment proxy) - use the outbound guard's client", file)
		}
		if (uses["http-client"] > 0 || uses["http-transport"] > 0) && !allowed[file] {
			t.Errorf("%s builds its own HTTP client - use newOutboundClient so the address is checked", file)
		}
	}
}

// The scanner still sees each construct it is meant to, so a silent regexp
// break cannot turn the inventory into an empty pass.
func TestEgressScannerStillDetects(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n" +
		"var a = &http.Client{}\nvar b = &http.Transport{}\nvar c = http.DefaultClient\n" +
		"func f() { http.NewRequest(\"GET\", \"u\", nil); http.Get(\"u\"); net.Dial(\"tcp\", \"a\"); smtp.SendMail(\"a\", nil, \"\", nil, nil); net.LookupHost(\"h\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := scanEgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	for kind := range egressKinds {
		if found["x.go"][kind] != 1 {
			t.Errorf("the scanner no longer finds %s: %v", kind, found["x.go"])
		}
	}
}
