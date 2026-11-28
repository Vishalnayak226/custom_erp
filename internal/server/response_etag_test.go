package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 297.8: the catalog's revalidation, through the real compression layer.
func TestWriteJSONRevalidatedAnswers304WhenUnchanged(t *testing.T) {
	type entry struct{ ID, Label string }
	list := make([]entry, 0, 300)
	for i := 0; i < 300; i++ {
		list = append(list, entry{ID: "r" + strings.Repeat("x", i%7), Label: "A report label long enough to compress"})
	}
	var current any = list
	h := compressResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store") // what apiMiddleware sets first
		writeJSONRevalidated(w, r, current)
	}))
	get := func(ifNoneMatch string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/catalog", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Result()
	}

	first := get("")
	tag := first.Header.Get("ETag")
	if first.StatusCode != http.StatusOK || !strings.HasPrefix(tag, `W/"`) {
		t.Fatalf("first answer: status %d, ETag %q; want 200 and a weak tag", first.StatusCode, tag)
	}
	if cc := first.Header.Get("Cache-Control"); cc != "private, no-cache" {
		t.Fatalf("Cache-Control %q, want private, no-cache (it must replace the middleware's no-store)", cc)
	}
	if first.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("a large answer should still be compressed")
	}
	zr, err := gzip.NewReader(first.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got []entry
	if err := json.NewDecoder(zr).Decode(&got); err != nil || len(got) != len(list) {
		t.Fatalf("body: %d entries, err %v; want %d", len(got), err, len(list))
	}

	for _, inm := range []string{tag, strings.TrimPrefix(tag, "W/"), `W/"other", ` + tag, "*"} {
		again := get(inm)
		body, _ := io.ReadAll(again.Body)
		if again.StatusCode != http.StatusNotModified || len(body) != 0 {
			t.Fatalf("If-None-Match %q: status %d with %d bytes; want 304 with no body", inm, again.StatusCode, len(body))
		}
		if again.Header.Get("ETag") != tag || again.Header.Get("Content-Encoding") != "" {
			t.Fatalf("304 headers: ETag %q, Content-Encoding %q", again.Header.Get("ETag"), again.Header.Get("Content-Encoding"))
		}
	}

	// A different list (another role, a module switched off, a release) is a
	// different tag, so the old tag gets the whole new body.
	current = list[:10]
	changed := get(tag)
	if changed.StatusCode != http.StatusOK || changed.Header.Get("ETag") == tag {
		t.Fatalf("changed list: status %d, ETag %q; want 200 with a new tag", changed.StatusCode, changed.Header.Get("ETag"))
	}
}

func TestETagMatches(t *testing.T) {
	const tag = `W/"abc"`
	for _, c := range []struct {
		header string
		want   bool
	}{
		{"", false},
		{`W/"abc"`, true},
		{`"abc"`, true},
		{`W/"abd"`, false},
		{`W/"x", W/"abc"`, true},
		{`W/"x",W/"y"`, false},
		{"*", true},
	} {
		if got := etagMatches(c.header, tag); got != c.want {
			t.Errorf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}
