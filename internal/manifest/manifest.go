// Package manifest describes the CashCtrl API surface the CLI exposes.
//
// The manifest is generated from the vendored spec (spec/cashctrl-api.json),
// gzipped, and embedded into the binary. At runtime the CLI parses it to build
// commands, render help, and classify operations by risk.
package manifest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SchemaVersion is the manifest format version this build understands.
// Parse rejects manifests declaring any other version.
const SchemaVersion = 1

// Risk classes for an operation, used to decide which operations need
// confirmation before they run.
const (
	RiskRead   = "read"
	RiskWrite  = "write"
	RiskDelete = "delete"
	RiskSend   = "send"
)

// Pagination styles. CashCtrl list endpoints page with start/limit and answer
// with a {"total": n, "data": [...]} envelope.
const (
	PagStartLimit = "start-limit"
	PagNone       = "none"
)

// Response payload kinds. Everything that is not .json — CSV/PDF/Excel
// exports, ZIP bundles, file contents — is binary: passed through byte for
// byte, never appended to.
const (
	RespJSON   = "json"
	RespBinary = "binary"
)

// Manifest is the full set of operations the CLI exposes.
type Manifest struct {
	SchemaVersion int         `json:"schema_version"`
	Operations    []Operation `json:"operations"`
}

// Operation is a single CLI command bound to one API endpoint.
type Operation struct {
	Command []string `json:"command"` // e.g. ["person","create"]
	Method  string   `json:"method"`  // "GET" or "POST"
	Path    string   `json:"path"`    // relative to /api/v1, e.g. /person/create.json
	Group   string   `json:"group"`   // the docs' breadcrumb, e.g. "Person"
	Summary string   `json:"summary"`
	Doc     string   `json:"doc,omitempty"`
	Risk    string   `json:"risk"`
	// Query holds the documented parameters of a GET operation, exposed as
	// flags. POST parameters travel in Body instead: CashCtrl POSTs are
	// form-encoded, and the CLI builds the form from --data JSON.
	Query      []Param `json:"query,omitempty"`
	Body       *Body   `json:"body,omitempty"`
	Pagination string  `json:"pagination"`
	Response   string  `json:"response"`
}

// Param is one documented parameter. CashCtrl documents nested JSON values
// (arrays or objects passed as a JSON string inside one form field or query
// parameter) with sub-parameter tables, carried in Sub.
type Param struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // TEXT NUMBER BOOLEAN CSV DATE JSON XML HTML
	Required bool     `json:"required"`
	Doc      string   `json:"doc,omitempty"`
	Values   []string `json:"values,omitempty"`
	Sub      []Param  `json:"sub,omitempty"`
}

// Body describes a POST operation's form-encoded body.
type Body struct {
	// Required is true when at least one field is mandatory, which is when the
	// CLI insists on --data. The docs mark conditionally-required fields
	// ("either firstName or company") as mandatory too, so field-level
	// validation stays with the API; this only gates the empty body.
	Required bool    `json:"required"`
	Fields   []Param `json:"fields,omitempty"`
	Example  string  `json:"example,omitempty"`
}

// Parse gunzips and decodes a manifest, rejecting any schema version this
// build does not understand.
func Parse(gz []byte) (*Manifest, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	defer zr.Close()

	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("manifest: schema_version %d, want %d", m.SchemaVersion, SchemaVersion)
	}
	return &m, nil
}

// Find returns the operation matching method and path exactly, or nil.
// CashCtrl paths carry no placeholders (ids travel as parameters), so lookup
// is literal; a trailing slash and any query string are ignored.
func (m *Manifest) Find(method, path string) *Operation {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimRight(path, "/")
	for i := range m.Operations {
		op := &m.Operations[i]
		if op.Method == method && op.Path == path {
			return op
		}
	}
	return nil
}
