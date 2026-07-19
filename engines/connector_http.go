package engines

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// doConnectorRequest makes a single outbound HTTP call to an external
// commerce platform, structurally identical to engines/extensions.go's
// callHookWithRecovery: a timeout-bounded http.Client, the actual call
// wrapped in its own recover()'d goroutine, and a hard safety-margin
// timeout on top of the client's own - so a hanging or panicking external
// API call can never block or crash the publish-queue worker.
func doConnectorRequest(ctx context.Context, timeout time.Duration, method, url string, headers map[string]string, body []byte) (status int, respBody []byte, err error) {
	type result struct {
		status int
		body   []byte
		err    error
	}
	resultCh := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- result{0, nil, fmt.Errorf("connector call panicked: %v", r)}
			}
		}()

		client := &http.Client{Timeout: timeout}
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, url, reader)
		if reqErr != nil {
			resultCh <- result{0, nil, reqErr}
			return
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, doErr := client.Do(req)
		if doErr != nil {
			resultCh <- result{0, nil, doErr}
			return
		}
		defer resp.Body.Close()
		respBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			resultCh <- result{resp.StatusCode, nil, readErr}
			return
		}
		resultCh <- result{resp.StatusCode, respBytes, nil}
	}()

	select {
	case res := <-resultCh:
		return res.status, res.body, res.err
	case <-time.After(timeout + 5*time.Second):
		// Safety margin over the http.Client's own timeout - should be
		// unreachable in practice, guarantees the caller is never blocked
		// forever regardless of what a misbehaving platform API does.
		return 0, nil, fmt.Errorf("connector call to %s exceeded safety timeout", url)
	}
}

// Per-channel outbound rate limiter - a simple token bucket keyed by
// channel_code, the mirror image of main.go's inbound globalLimiter (that
// one throttles calls made INTO this app; this one throttles calls THIS
// app makes OUT to a platform, so a busy publish queue can't blow through
// Shopify/BigCommerce/Magento's own rate limits and get the whole
// integration throttled or banned).
type tokenBucket struct {
	capacity    int
	tokens      int
	refillEvery time.Duration
	lastRefill  time.Time
}

var (
	connectorLimiterMu      sync.Mutex
	connectorLimiterBuckets = make(map[string]*tokenBucket)
)

// allowConnectorCall checks (and consumes from) a channel's outbound call
// budget. capacity/window are supplied by the calling connector - each
// platform declares its own (BigCommerce: e.g. 150 per 30s; Magento: a
// conservative default; Shopify additionally self-corrects using the
// GraphQL response's own cost data, see connector_shopify.go). Returns
// false if the budget is exhausted - the caller should leave the publish
// job Queued for the next worker tick rather than treating this as a
// failure.
func allowConnectorCall(channelCode string, capacity int, window time.Duration) bool {
	connectorLimiterMu.Lock()
	defer connectorLimiterMu.Unlock()

	b, ok := connectorLimiterBuckets[channelCode]
	if !ok {
		b = &tokenBucket{capacity: capacity, tokens: capacity, refillEvery: window, lastRefill: time.Now()}
		connectorLimiterBuckets[channelCode] = b
	}

	if time.Since(b.lastRefill) >= b.refillEvery {
		b.tokens = b.capacity
		b.lastRefill = time.Now()
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}
