package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ExternalResult struct {
	URL        string    `json:"url"`
	Sources    []string  `json:"sources"`
	CheckedOn  time.Time `json:"checked_on"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Result     string    `json:"result"`
	Cached     bool      `json:"cached"`
}

// A scheduled documentation check must never turn repository links into
// requests to local services, cloud metadata, credential URLs or private hosts.
func publicLink(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" {
		return nil, fmt.Errorf("only credential-free public HTTP(S) links without query strings are checked")
	}
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		return nil, fmt.Errorf("non-web port")
	}
	u.Fragment = ""
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return nil, fmt.Errorf("nonpublic address")
	}
	if strings.EqualFold(u.Hostname(), "localhost") || !strings.Contains(u.Hostname(), ".") {
		return nil, fmt.Errorf("local host")
	}
	return u, nil
}

func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Includes shared-address/metadata networks not covered by net.IP.IsPrivate.
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "2001:db8::/32", "2001::/32", "2002::/16"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

func externalClient() *http.Client {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no address")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, fmt.Errorf("nonpublic address")
			}
		}
		// Dial the validated address itself to avoid a second DNS lookup/rebinding.
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	return &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return fmt.Errorf("redirect limit")
		}
		_, err := publicLink(req.URL.String())
		return err
	}}
}

func externalReport(root, out, cachePath string, now time.Time) error {
	if filepath.Clean(out) == filepath.Clean(cachePath) {
		return fmt.Errorf("report and cache must be distinct files")
	}
	links := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, body := frontmatter(strings.ReplaceAll(string(raw), "\r\n", "\n"))
		body = stripInlineCode(stripCode(body))
		rel, _ := filepath.Rel(root, path)
		matches := append(inlineLink.FindAllStringSubmatch(body, -1), referenceLink.FindAllStringSubmatch(body, -1)...)
		for _, match := range matches {
			u, err := publicLink(strings.Trim(match[1], "<>"))
			if err != nil {
				continue
			}
			key := u.String()
			if !contains(links[key], filepath.ToSlash(rel)) {
				links[key] = append(links[key], filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	cache := map[string]ExternalResult{}
	if raw, err := os.ReadFile(cachePath); err == nil {
		if err = json.Unmarshal(raw, &cache); err != nil {
			return fmt.Errorf("invalid external-link cache")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	keys := make([]string, 0, len(links))
	for key := range links {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]ExternalResult, len(keys))
	client := externalClient()
	defer client.CloseIdleConnections()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				key := keys[i]
				old := cache[key]
				if old.URL == key && now.Sub(old.CheckedOn) >= 0 && now.Sub(old.CheckedOn) < 7*24*time.Hour {
					old.Sources = links[key]
					old.Cached = true
					results[i] = old
					continue
				}
				entry := ExternalResult{URL: key, Sources: links[key], CheckedOn: now, Result: "unreachable-or-blocked"}
				req, err := http.NewRequest(http.MethodGet, key, nil)
				if err == nil {
					req.Header.Set("User-Agent", "ERP-documentation-link-check/1.0")
					res, err := client.Do(req)
					if err == nil {
						entry.HTTPStatus = res.StatusCode
						_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
						res.Body.Close()
						switch {
						case res.StatusCode >= 200 && res.StatusCode < 400:
							entry.Result = "reachable"
						case res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 429:
							entry.Result = "manual-review"
						default:
							entry.Result = "http-error"
						}
					}
				}
				results[i] = entry
			}
		}()
	}
	for i := range keys {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	next := map[string]ExternalResult{}
	for _, r := range results {
		next[r.URL] = r
	}
	for path, value := range map[string]any{out: results, cachePath: next} {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(path, append(raw, '\n'), 0644); err != nil {
			return err
		}
	}
	fmt.Printf("doclint: %d public links reported; failures are advisory, excluded query/private links need owner review\n", len(results))
	return nil
}
