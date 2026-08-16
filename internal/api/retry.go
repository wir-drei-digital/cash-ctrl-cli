package api

import (
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// classifyTransport maps a transport error. Dial/DNS failures mean the request
// never reached the server, so they are transport errors and safe to retry.
// Anything later on a POST may have been processed: the caller must verify
// state rather than replay the mutation.
func classifyTransport(err error, method string) *Error {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	presend := (errors.As(err, &opErr) && opErr.Op == "dial") || errors.As(err, &dnsErr)
	if method == "GET" || presend {
		return &Error{Kind: KindTransport, Message: err.Error()}
	}
	return &Error{
		Kind: KindOutcomeUnknown,
		Message: "request may or may not have been processed: " + err.Error() +
			" — verify state before retrying",
	}
}

// retryAfter returns how long to wait after a 429: Retry-After seconds,
// else RateLimit-Reset seconds, else the fallback.
func retryAfter(h http.Header, fallback time.Duration) time.Duration {
	for _, k := range []string{"Retry-After", "RateLimit-Reset"} {
		if v := h.Get(k); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return fallback
}

// jitter spreads retries so concurrent clients do not resynchronize.
func jitter() time.Duration { return time.Duration(rand.Int63n(int64(500 * time.Millisecond))) }
