package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLive is a read-only check against a real CashCtrl organization,
// skipped unless explicitly requested:
//
//	CASHCTRL_LIVE=1 CASHCTRL_ORG=... CASHCTRL_API_KEY=... go test ./e2e -run TestLive -v
func TestLive(t *testing.T) {
	if os.Getenv("CASHCTRL_LIVE") != "1" {
		t.Skip("set CASHCTRL_LIVE=1 to run against production")
	}
	if os.Getenv("CASHCTRL_ORG") == "" || os.Getenv("CASHCTRL_API_KEY") == "" {
		t.Fatal("CASHCTRL_LIVE=1 needs CASHCTRL_ORG and CASHCTRL_API_KEY")
	}
	bin := buildBinary(t)

	stdout, stderr, code := runEnv(t, bin, os.Environ(), "auth", "verify")
	if code != 0 {
		t.Fatalf("auth verify: exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, `"ok":true`) {
		t.Fatalf("stdout %q", stdout)
	}

	stdout, stderr, code = runEnv(t, bin, os.Environ(), "fiscalperiod", "list")
	if code != 0 {
		t.Fatalf("fiscalperiod list: exit %d: %s", code, stderr)
	}
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("response not a data envelope (%v): %.200s", err, stdout)
	}
}
