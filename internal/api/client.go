// Package api is the HTTP client for the CashCtrl API. It owns the safety
// guardrails (credential routing, HTTPS, read-only mode) and the retry policy.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// Client performs API requests. The zero value is unusable; BaseURL and APIKey
// are required. Timeout, RetryBudget and MaxAttempts fall back to defaults.
type Client struct {
	// BaseURL is the full API base including /api/v1, normally
	// https://<org>.cashctrl.com/api/v1. Empty means no org is configured,
	// which guard turns into a usage error naming the ways to set one.
	BaseURL string
	// APIKey authenticates every request as HTTP Basic auth username with an
	// empty password, per CashCtrl's contract. Empty means no credentials.
	APIKey                    string
	ReadOnly, AllowCustomBase bool
	// Lang, when set, travels as the lang query parameter on every request.
	// It selects the language of error messages and generated documents.
	Lang        string
	Timeout     time.Duration // per attempt; default 30s
	RetryBudget time.Duration // total 429 wait; default 60s
	MaxAttempts int           // GET transient attempts; default 3
	Verbose     io.Writer     // nil = silent; never receives the API key
	HTTP        *http.Client
	Sleep       func(time.Duration)
}

// Request is one API call.
type Request struct {
	Method string // GET or POST
	Path   string // relative to BaseURL, e.g. /person/list.json
	Query  url.Values
	// Form is the form-encoded POST body. CashCtrl accepts form-encoded
	// bodies only; the CLI builds this from --data JSON.
	Form url.Values
	Risk string // manifest.Risk*
	// Headers are extra request headers, applied after the defaults so a
	// caller can replace Accept — but never Authorization, which the client
	// owns.
	Headers http.Header
}

// Response is a successful (2xx) API response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (c *Client) logf(format string, a ...any) {
	if c.Verbose != nil {
		fmt.Fprintf(c.Verbose, format+"\n", a...)
	}
}

// guard rejects a request locally, before anything is sent.
func (c *Client) guard(r Request) *Error {
	if c.APIKey == "" {
		return Usagef("no credentials: set CASHCTRL_API_KEY, or store a key with `echo $KEY | cashctrl config set api-key`")
	}
	if c.BaseURL == "" {
		return Usagef("no organization: set CASHCTRL_ORG, or store one with `cashctrl config set org <org>`")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return Usagef("invalid base URL %q", c.BaseURL)
	}
	host := u.Hostname()
	if !strings.HasSuffix(host, ".cashctrl.com") && !c.AllowCustomBase {
		return Usagef("refusing to send the API key to %s; set CASHCTRL_ALLOW_CUSTOM_BASE=1 if intentional", host)
	}
	if u.Scheme != "https" && !isLoopback(host) {
		return Usagef("refusing non-HTTPS base %s", c.BaseURL)
	}
	if c.ReadOnly && r.Risk != manifest.RiskRead {
		return Usagef("read-only mode (CASHCTRL_READ_ONLY) blocks %s %s", r.Method, r.Path)
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// redirectPolicy is the client's redirect handling. CashCtrl redirects GET
// downloads (file contents, payment files) to a storage host by design, so
// GETs follow up to a few hops — but only to HTTPS targets (or loopback, for
// tests), because Go re-sends the request there and a downgrade would put
// plaintext on the wire. Go itself strips the Authorization header when the
// redirect leaves the API host, so the key cannot follow the download.
//
// A redirected POST is never followed: a 307/308 replays the body, which for
// a mutation means sending it a second time — the one thing the retry policy
// is built to never do.
func redirectPolicy(req *http.Request, via []*http.Request) error {
	if via[0].Method != "GET" {
		return http.ErrUseLastResponse
	}
	if len(via) >= 5 {
		return fmt.Errorf("stopped after 5 redirects")
	}
	if req.URL.Scheme != "https" && !isLoopback(req.URL.Hostname()) {
		return fmt.Errorf("refusing redirect to non-HTTPS target %s", req.URL)
	}
	return nil
}

// attempt performs one HTTP round trip. Do wraps it with the retry loop.
func (c *Client) attempt(ctx context.Context, r Request) (*Response, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	q := url.Values{}
	for k, vs := range r.Query {
		q[k] = vs
	}
	// lang selects the language of error messages and generated documents; an
	// explicit per-request value wins over the configured one.
	if c.Lang != "" && q.Get("lang") == "" {
		q.Set("lang", c.Lang)
	}
	u := strings.TrimRight(c.BaseURL, "/") + r.Path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var body io.Reader
	if r.Form != nil {
		body = strings.NewReader(r.Form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		return nil, &Error{Kind: KindUsage, Message: err.Error()}
	}
	// The API key is the basic auth username; the password is empty.
	req.SetBasicAuth(c.APIKey, "")
	req.Header.Set("Accept", "application/json")
	if r.Form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, vs := range r.Headers {
		// Authorization is the client's alone: an escape-hatch caller must not
		// be able to send this key — or any other credential — to a header of
		// its choosing.
		if http.CanonicalHeaderKey(k) == "Authorization" {
			continue
		}
		req.Header[http.CanonicalHeaderKey(k)] = vs
	}

	c.logf("> %s %s", r.Method, r.Path)
	httpClient := &http.Client{}
	if c.HTTP != nil {
		cp := *c.HTTP // copy, so a caller's client keeps its own settings intact
		httpClient = &cp
	}
	// The redirect policy is the client's, not the caller's: see redirectPolicy.
	httpClient.CheckRedirect = redirectPolicy
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err // classified by Do
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.logf("< %d (%d bytes)", resp.StatusCode, len(raw))
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: raw}, nil
}

func errorDetails(resp *Response) any {
	if len(resp.Body) == 0 {
		return nil
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		var v any
		if json.Unmarshal(resp.Body, &v) == nil {
			return v
		}
	}
	return string(resp.Body)
}

// successFalse detects CashCtrl's in-band validation failure: HTTP 200 whose
// JSON body carries "success": false. Without this check a rejected create
// would exit 0 with the refusal on stdout, which an agent reads as success.
// Only an explicit false trips it — list envelopes and plain data responses
// carry no success field and pass through untouched.
func successFalse(r Request, resp *Response) *Error {
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return nil
	}
	trimmed := strings.TrimSpace(string(resp.Body))
	if !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	var probe struct {
		Success *bool `json:"success"`
		Errors  []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
		Message string `json:"message"`
	}
	if json.Unmarshal(resp.Body, &probe) != nil || probe.Success == nil || *probe.Success {
		return nil
	}
	msg := fmt.Sprintf("%s %s: the API reports success=false", r.Method, r.Path)
	switch {
	case len(probe.Errors) > 0 && probe.Errors[0].Field != "":
		msg += fmt.Sprintf(" (%s: %s)", probe.Errors[0].Field, probe.Errors[0].Message)
	case len(probe.Errors) > 0:
		msg += fmt.Sprintf(" (%s)", probe.Errors[0].Message)
	case probe.Message != "":
		msg += fmt.Sprintf(" (%s)", probe.Message)
	}
	return &Error{Kind: KindValidation, Message: msg, Status: resp.Status, Details: errorDetails(resp)}
}

// Do sends a request, applying the retry policy: 429 is retried for every
// operation within the retry budget; transient network and 5xx failures are
// retried for GET only. Mutations are never replayed. The returned error is
// always *Error.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	if err := c.guard(r); err != nil {
		return nil, err
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	budget := c.RetryBudget
	if budget == 0 {
		budget = 60 * time.Second
	}
	maxAttempts := c.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}

	var waited time.Duration
	transientTries := 0
	backoff := time.Second
	for {
		resp, err := c.attempt(ctx, r)
		if err != nil {
			// attempt already classified it: the request was never built, so
			// it was provably never sent and must not be retried.
			var apiErr *Error
			if errors.As(err, &apiErr) {
				return nil, apiErr
			}
			e := classifyTransport(err, r.Method)
			if e.Kind == KindTransport && r.Method == "GET" {
				transientTries++
				if transientTries < maxAttempts {
					sleep(backoff)
					backoff *= 2
					continue
				}
			}
			return nil, e
		}
		switch {
		case resp.Status >= 200 && resp.Status < 300:
			if e := successFalse(r, resp); e != nil {
				return nil, e
			}
			return resp, nil
		case resp.Status >= 300 && resp.Status < 400:
			// Reaching here means the redirect policy refused to follow: a
			// redirected POST, too many hops, or a non-HTTPS target. Kind
			// "validation" rather than "server": the request was answered in
			// full and refused by our own policy, so it is the request that
			// has to change. Naming the Location tells the caller where the
			// server pointed.
			return nil, &Error{
				Kind: KindValidation,
				Message: fmt.Sprintf("%s %s: HTTP %d redirect to %q — not followed: "+
					"POSTs are never replayed and downloads only follow HTTPS targets",
					r.Method, r.Path, resp.Status, resp.Header.Get("Location")),
				Status:  resp.Status,
				Details: errorDetails(resp),
			}
		case resp.Status == 429:
			// Floor the wait at the current backoff so a server answering
			// "Retry-After: 0" cannot pull us into a sub-second retry storm;
			// a longer header-derived wait still wins.
			wait := max(retryAfter(resp.Header, backoff), backoff) + jitter()
			if waited+wait > budget {
				return nil, &Error{
					Kind:    KindRateLimited,
					Message: fmt.Sprintf("%s %s: rate limited, retry budget exhausted", r.Method, r.Path),
					Status:  429,
					Details: errorDetails(resp),
				}
			}
			waited += wait
			c.logf("* 429, waiting %s", wait)
			sleep(wait)
			backoff *= 2
			continue
		case resp.Status >= 500 && r.Method == "GET":
			transientTries++
			if transientTries < maxAttempts {
				sleep(backoff)
				backoff *= 2
				continue
			}
		}
		msg := fmt.Sprintf("%s %s: HTTP %d", r.Method, r.Path, resp.Status)
		if resp.Status == 403 {
			// CashCtrl permissions live on the API user's role, not on the
			// key: no flag on this side can widen them.
			msg += " — the API user's role lacks permission for this operation; adjust it under Settings > Users & Roles"
		}
		return nil, &Error{
			Kind:    kindForStatus(resp.Status),
			Message: msg,
			Status:  resp.Status,
			Details: errorDetails(resp),
		}
	}
}

// PutFile uploads bytes to a presigned storage URL from file prepare — the
// one request that leaves the API host by design. It sends no credentials
// (the URL itself is the authorization) and insists on HTTPS except toward
// loopback, which exists for tests.
func (c *Client) PutFile(ctx context.Context, dst, contentType string, body []byte) error {
	u, err := url.Parse(dst)
	if err != nil || (u.Scheme != "https" && !isLoopback(u.Hostname())) {
		return &Error{Kind: KindUsage, Message: fmt.Sprintf("refusing upload to %q: not an HTTPS URL", dst)}
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute // uploads carry real payloads; 30s is too tight
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, dst, strings.NewReader(string(body)))
	if err != nil {
		return &Error{Kind: KindUsage, Message: err.Error()}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.logf("> PUT %s://%s/… (%d bytes)", u.Scheme, u.Host, len(body))
	httpClient := &http.Client{}
	if c.HTTP != nil {
		cp := *c.HTTP
		httpClient = &cp
	}
	httpClient.CheckRedirect = redirectPolicy
	resp, err := httpClient.Do(req)
	if err != nil {
		return classifyTransport(err, http.MethodPut)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	c.logf("< %d", resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			Kind:    kindForStatus(resp.StatusCode),
			Message: fmt.Sprintf("PUT upload: HTTP %d", resp.StatusCode),
			Status:  resp.StatusCode,
			Details: string(raw),
		}
	}
	return nil
}
