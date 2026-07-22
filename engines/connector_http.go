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
//
// breakerKey (24.29) gates the call through a small stdlib-only circuit
// breaker keyed per platform (each call site passes its own platform name -
// "shopify"/"bigcommerce"/"magento" - not per-shop, since no connector sees
// live production traffic yet per this stage's own scoping note; revisit
// granularity once one does). Deliberately not gobreaker or any other
// dependency, matching this repo's lightweight-first principle.
func doConnectorRequest(ctx context.Context, timeout time.Duration, method, url string, headers map[string]string, body []byte, breakerKey string) (status int, respBody []byte, err error) {
	if !circuitBreakerAllow(breakerKey) {
		return 0, nil, fmt.Errorf("circuit breaker open for %q - too many recent failures, not attempting this call", breakerKey)
	}

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
		// Breaker failure = transport-level error or a 5xx (the platform
		// itself struggling) - a 4xx means this app's own request was
		// wrong, which retrying identically won't fix and isn't the
		// platform-availability signal a breaker exists to catch.
		circuitBreakerRecord(breakerKey, res.err == nil && res.status < 500)
		return res.status, res.body, res.err
	case <-time.After(timeout + 5*time.Second):
		// Safety margin over the http.Client's own timeout - should be
		// unreachable in practice, guarantees the caller is never blocked
		// forever regardless of what a misbehaving platform API does.
		circuitBreakerRecord(breakerKey, false)
		return 0, nil, fmt.Errorf("connector call to %s exceeded safety timeout", url)
	}
}

// circuitBreakerState tracks consecutive failures per breakerKey (24.29,
// loophole #33). A small stdlib-only breaker - sync.Mutex + time, no
// gobreaker dependency - per this stage's scoping note. Opens (stops
// attempting calls) after circuitBreakerFailureThreshold consecutive
// failures, and self-resets (half-open: the next call is allowed through
// as a trial) once circuitBreakerCooldown has elapsed since it opened.
type circuitBreakerEntry struct {
	consecutiveFailures int
	openUntil           time.Time
}

const (
	circuitBreakerFailureThreshold = 5
	circuitBreakerCooldown         = 30 * time.Second
)

var (
	circuitBreakerMu    sync.Mutex
	circuitBreakerState = make(map[string]*circuitBreakerEntry)
)

// circuitBreakerAllow reports whether a call for this key may proceed. A
// key with no failure history, or one below threshold, always proceeds; a
// key that tripped the breaker is blocked until its cooldown elapses, at
// which point exactly one trial call is let through (the entry itself
// isn't reset here - only circuitBreakerRecord, on that trial's actual
// outcome, decides whether to close the breaker again or re-open it).
func circuitBreakerAllow(breakerKey string) bool {
	circuitBreakerMu.Lock()
	defer circuitBreakerMu.Unlock()
	entry, ok := circuitBreakerState[breakerKey]
	if !ok || entry.consecutiveFailures < circuitBreakerFailureThreshold {
		return true
	}
	return time.Now().After(entry.openUntil)
}

// circuitBreakerRecord updates breakerKey's failure streak after a call
// completes. A success resets the streak to zero (closed); a failure
// increments it and, once it crosses the threshold, sets/refreshes
// openUntil so circuitBreakerAllow blocks further attempts until cooldown.
func circuitBreakerRecord(breakerKey string, success bool) {
	circuitBreakerMu.Lock()
	defer circuitBreakerMu.Unlock()
	entry, ok := circuitBreakerState[breakerKey]
	if !ok {
		entry = &circuitBreakerEntry{}
		circuitBreakerState[breakerKey] = entry
	}
	if success {
		entry.consecutiveFailures = 0
		entry.openUntil = time.Time{}
		return
	}
	entry.consecutiveFailures++
	if entry.consecutiveFailures >= circuitBreakerFailureThreshold {
		entry.openUntil = time.Now().Add(circuitBreakerCooldown)
	}
}

// Per-channel outbound rate limiter - a simple token bucket keyed by
// channel_code, the mirror image of internal/server/middleware.go's inbound globalLimiter (that
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
