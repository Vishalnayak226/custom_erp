package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Session is one signed-in role. The tool signs in the way a person does
// (login, then change-password on first use) and signs in again when the token
// expires, so a long run does not die of a 15-minute token.
type Session struct {
	Name     string
	Role     string
	Username string
	Password string
	Token    string
	Mint     func() string // signs a fresh token without a password (the admin session)
}

// Client records every call it makes, with the role that made it.
type Client struct {
	Base    string
	HTTP    *http.Client
	mu      sync.Mutex
	calls   []Call
	step    string
	Retries int // attempts after the first, for transport errors and 429/502/503
	Sess    map[string]*Session
	logins  []time.Time // recent login times, to stay under the server's 5 per minute
}

func NewClient(base string) *Client {
	return &Client{
		Base:    strings.TrimRight(base, "/"),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Retries: 3,
		Sess:    map[string]*Session{},
	}
}

// SetStep labels the calls that follow.
func (c *Client) SetStep(s string) { c.mu.Lock(); c.step = s; c.mu.Unlock() }

// Take returns and clears the recorded calls.
func (c *Client) Take() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.calls
	c.calls = nil
	return out
}

// Resp is a decoded response.
type Resp struct {
	Status int
	Body   []byte
	JSON   map[string]interface{}
	Arr    []interface{}
	Header http.Header
}

// CallOpts tunes one call.
type CallOpts struct {
	Expect    []int // statuses that are not errors (a refusal the step wants)
	Setup     bool  // not a business write: not counted toward per-1k figures
	Multipart *multipartBody
	NoRecord  bool
	Headers   map[string]string // extra request headers (an Idempotency-Key)
}

type multipartBody struct {
	Field, Filename string
	Content         []byte
	ContentType     string
}

func (o CallOpts) expected(code int) bool {
	for _, e := range o.Expect {
		if e == code {
			return true
		}
	}
	return false
}

// Do sends one request as the named session.
func (c *Client) Do(who, method, path string, body interface{}, opts CallOpts) (*Resp, error) {
	var reqBytes []byte
	ctype := "application/json"
	if opts.Multipart != nil {
		var buf bytes.Buffer
		ctype = buildMultipart(&buf, opts.Multipart)
		reqBytes = buf.Bytes()
	} else if body != nil {
		var err error
		if s, ok := body.(string); ok {
			reqBytes = []byte(s)
		} else if reqBytes, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	sess := c.Sess[who]
	var last *Resp
	var lastErr error
	retries := 0
	start := time.Now()
	refreshed := false
	for attempt := 0; attempt <= c.Retries; attempt++ {
		req, err := http.NewRequest(method, c.Base+path, bytes.NewReader(reqBytes))
		if err != nil {
			return nil, err
		}
		if reqBytes != nil {
			req.Header.Set("Content-Type", ctype)
		}
		for k, v := range opts.Headers {
			req.Header.Set(k, v)
		}
		if sess != nil && sess.Token != "" {
			req.Header.Set("Authorization", "Bearer "+sess.Token)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			retries++
			time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		r := &Resp{Status: resp.StatusCode, Body: b, Header: resp.Header}
		if len(b) > 0 && (b[0] == '{' || b[0] == '[') {
			if b[0] == '{' {
				_ = json.Unmarshal(b, &r.JSON)
			} else {
				_ = json.Unmarshal(b, &r.Arr)
			}
		}
		last, lastErr = r, nil
		if r.Status == 401 && sess != nil && !refreshed && !strings.HasSuffix(path, "/login") {
			if sess.Mint != nil {
				refreshed = true
				sess.Token = sess.Mint()
				retries++
				continue
			}
			if sess.Username != "" {
				refreshed = true
				if err := c.Login(sess); err == nil {
					retries++
					continue
				}
			}
		}
		if r.Status == 429 || r.Status == 502 || r.Status == 503 {
			retries++
			wait := 1500 * time.Millisecond
			time.Sleep(wait)
			continue
		}
		break
	}
	ms := float64(time.Since(start).Microseconds()) / 1000
	call := Call{Step: c.step, Role: who, Method: method, Path: pathOnly(path), MS: round2(ms), ReqBytes: int64(len(reqBytes)), Retries: retries}
	if lastErr != nil && last == nil {
		call.Err = lastErr.Error()
	} else {
		call.Status = last.Status
		call.RespBytes = int64(len(last.Body))
		call.Expected = opts.expected(last.Status)
		call.CountsAsTx = method != "GET" && !opts.Setup && last.Status < 300
	}
	if !opts.NoRecord {
		c.mu.Lock()
		c.calls = append(c.calls, call)
		c.mu.Unlock()
	}
	if last == nil {
		return nil, lastErr
	}
	return last, nil
}

func pathOnly(p string) string {
	if i := strings.Index(p, "?"); i >= 0 {
		return p[:i]
	}
	return p
}

// Login signs the session in. Not recorded as a business call.
func (c *Client) Login(s *Session) error {
	c.paceLogin()
	saved := c.Sess[s.Name]
	tmp := *s
	tmp.Token = ""
	c.Sess["__login"] = &tmp
	defer delete(c.Sess, "__login")
	r, err := c.Do("__login", "POST", "/api/v1/login", map[string]string{"username": s.Username, "password": s.Password}, CallOpts{Setup: true, NoRecord: true})
	if err != nil {
		return err
	}
	if r.Status != 200 {
		return fmt.Errorf("login %s: %d %s", s.Username, r.Status, trunc(string(r.Body), 120))
	}
	tok, _ := r.JSON["token"].(string)
	if tok == "" {
		return fmt.Errorf("login %s: no token", s.Username)
	}
	s.Token = tok
	if saved != nil {
		saved.Token = tok
	}
	return nil
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---- small JSON accessors over decoded responses ----

func jstr(m map[string]interface{}, keys ...string) string {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	case nil:
		return ""
	}
	return fmt.Sprintf("%v", cur)
}

func jnum(m map[string]interface{}, keys ...string) float64 {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return 0
		}
		cur = mm[k]
	}
	f, _ := cur.(float64)
	return f
}

func buildMultipart(buf *bytes.Buffer, mp *multipartBody) string {
	boundary := "journeylab0123456789"
	ct := mp.ContentType
	if ct == "" {
		ct = "text/csv"
	}
	fmt.Fprintf(buf, "--%s\r\nContent-Disposition: form-data; name=%q; filename=%q\r\nContent-Type: %s\r\n\r\n", boundary, mp.Field, mp.Filename, ct)
	buf.Write(mp.Content)
	fmt.Fprintf(buf, "\r\n--%s--\r\n", boundary)
	return "multipart/form-data; boundary=" + boundary
}

// paceLogin waits when five logins already happened in the last minute: the
// server limits sign-ins to 5 a minute per address, and a journey run signs in
// several roles. This is the only deliberate sleep in the client.
func (c *Client) paceLogin() {
	const limit, window = 5, 62 * time.Second
	now := time.Now()
	keep := c.logins[:0]
	for _, t := range c.logins {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	c.logins = keep
	if len(c.logins) >= limit {
		time.Sleep(window - now.Sub(c.logins[0]))
	}
	c.logins = append(c.logins, time.Now())
}
