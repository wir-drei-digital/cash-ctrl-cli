package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
)

// maxJSONBody caps --data so a mistaken `--data @/dev/urandom` fails locally
// instead of streaming megabytes at the API.
const maxJSONBody = 20 << 20

// readJSONBody resolves --data: "-" reads stdin, "@path" reads a file, anything
// else is the literal body. Returns (nil, nil) when --data was not given.
func (a *app) readJSONBody(cmd *cobra.Command) ([]byte, error) {
	val, _ := cmd.Flags().GetString("data")
	if val == "" {
		return nil, nil
	}
	var raw []byte
	var err error
	switch {
	case val == "-":
		raw, err = io.ReadAll(io.LimitReader(a.stdin, maxJSONBody+1))
	case strings.HasPrefix(val, "@"):
		raw, err = os.ReadFile(val[1:])
	default:
		raw = []byte(val)
	}
	if err != nil {
		return nil, api.Usagef("reading --data: %v", err)
	}
	if len(raw) > maxJSONBody {
		return nil, api.Usagef("--data exceeds the 20MB limit")
	}
	if !json.Valid(raw) {
		return nil, api.Usagef("--data is not valid JSON")
	}
	return raw, nil
}

// formEncode turns a --data JSON object into the form-encoded parameters
// CashCtrl accepts: scalars go verbatim, nested arrays and objects are
// embedded as compact JSON strings — which is exactly how the API documents
// them ("This is a JSON array [{...},{...}]" inside one form field).
//
// The translation is mechanical and complete, so a field the manifest does
// not list still reaches the API.
func formEncode(raw []byte) (url.Values, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, api.Usagef("--data must be a JSON object with one key per API parameter")
	}
	form := url.Values{}
	// Deterministic order, so --verbose logs and tests see a stable body.
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := obj[k]
		var s string
		var asString string
		var asBool bool
		var asNumber json.Number
		dec := json.NewDecoder(strings.NewReader(string(v)))
		dec.UseNumber()
		switch {
		case string(v) == "null":
			continue // an explicitly absent value stays absent
		case json.Unmarshal(v, &asString) == nil:
			s = asString
		case json.Unmarshal(v, &asBool) == nil:
			s = fmt.Sprintf("%v", asBool)
		case dec.Decode(&asNumber) == nil:
			s = asNumber.String()
		default:
			// Arrays and objects travel as their compact JSON text.
			s = string(v)
		}
		form.Set(k, s)
	}
	return form, nil
}
