// Package spec defines the vendored description of the CashCtrl API surface.
//
// CashCtrl publishes no OpenAPI document; its API reference is a single
// generated HTML page. tools/fetchspec extracts that page into this format,
// which is committed under spec/ so a broken extraction can never break a
// build, and tools/genmanifest turns it into the embedded manifest.
package spec

import (
	"encoding/json"
	"fmt"
	"os"
)

// Version is the spec format version this build reads and writes.
const Version = 1

// Spec is the extracted API surface.
type Spec struct {
	SpecVersion int         `json:"spec_version"`
	Source      string      `json:"source"`
	Operations  []Operation `json:"operations"`
}

// Operation is one documented endpoint.
type Operation struct {
	Method string `json:"method"` // GET or POST
	Path   string `json:"path"`   // relative to /api/v1, e.g. /person/create.json
	Group  string `json:"group"`  // breadcrumb, e.g. "Person"
	Title  string `json:"title"`  // e.g. "Create person"
	Doc    string `json:"doc,omitempty"`
	// Params are the documented request parameters: query parameters for GET,
	// form-encoded body parameters for POST.
	Params []Param `json:"params,omitempty"`
}

// Param is one documented parameter. CashCtrl documents nested JSON structures
// (arrays of objects passed as a JSON string inside a form field) as
// sub-parameter tables, carried here in Sub.
type Param struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"` // TEXT NUMBER BOOLEAN CSV DATE JSON XML HTML
	Required bool    `json:"required"`
	Doc      string  `json:"doc,omitempty"`
	Values   []string `json:"values,omitempty"` // the docs' "Possible values" list
	Sub      []Param `json:"sub,omitempty"`
}

// Load reads and validates a spec file.
func Load(path string) (*Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.SpecVersion != Version {
		return nil, fmt.Errorf("%s: spec_version %d, want %d", path, s.SpecVersion, Version)
	}
	return &s, nil
}
