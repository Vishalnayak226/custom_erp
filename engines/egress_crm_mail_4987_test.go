package engines

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 49.8.7: a CRM email sender that names its own mail server is a
// client-typed address, so its connection goes through the outbound guard
// like every client-typed HTTP call - it was a plain dial, which let a sender
// point at an internal address and port. The server's own SMTP_HOST relay
// (the operator's setting) is dialed as before.
func TestCRMMailServerDialIsGuarded(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var accepted atomic.Int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			c.Close() // no greeting: the send fails after the connection, which is all this needs
		}
	}()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	sender := &crmSenderRow{CRMSender: CRMSender{Provider: CRMProviderEmail, Channel: "email",
		Config: map[string]interface{}{"host": "127.0.0.1", "port": port, "from_email": "crm@example.com"}}}
	d := crmDispatch{SendID: "S-4987", Channel: "email", To: "someone@example.com", Subject: "s", Body: "b"}

	was := outboundAllowPrivate
	defer func() { outboundAllowPrivate = was }()
	outboundAllowPrivate = false
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = crmSendEmail(ctx, sender, d)
	var notSent *errCRMNotSent
	if !errors.As(err, &notSent) || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("a sender's own loopback mail server is refused before connecting: %v", err)
	}
	if accepted.Load() != 0 {
		t.Fatal("the refused dial must not reach the address")
	}

	// The on-premise switch still lets an install use a mail server on its
	// own network.
	outboundAllowPrivate = true
	if _, err = crmSendEmail(ctx, sender, d); err == nil {
		t.Fatal("a server that never greets cannot be a successful send")
	}
	deadline := time.Now().Add(2 * time.Second)
	for accepted.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if accepted.Load() != 1 {
		t.Fatalf("with OUTBOUND_ALLOW_PRIVATE the mail server is reached: %d connections", accepted.Load())
	}
}
