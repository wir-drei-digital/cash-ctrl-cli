package main

import (
	"encoding/json"
	"strings"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
)

// docLimit bounds prose carried into the manifest so the embedded file stays
// small; the full text remains in the vendored spec and the online docs.
const docLimit = 400

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}

// params converts spec parameters (recursively, sub-tables included) into
// manifest parameters.
func params(in []spec.Param) []manifest.Param {
	var out []manifest.Param
	for _, p := range in {
		out = append(out, manifest.Param{
			Name: p.Name, Type: p.Type, Required: p.Required,
			Doc: truncate(p.Doc, docLimit), Values: p.Values, Sub: params(p.Sub),
		})
	}
	return out
}

// pagination marks GET operations that document both start and limit: those
// answer with a {"total", "data"} envelope the CLI knows how to walk.
func pagination(op spec.Operation) string {
	if op.Method != "GET" {
		return manifest.PagNone
	}
	var hasStart, hasLimit bool
	for _, p := range op.Params {
		switch p.Name {
		case "start":
			hasStart = true
		case "limit":
			hasLimit = true
		}
	}
	if hasStart && hasLimit {
		return manifest.PagStartLimit
	}
	return manifest.PagNone
}

// responseKind: only .json endpoints answer JSON; every other extension (or
// none — file contents, logos, payment files) is a download passed through
// byte for byte.
func responseKind(op spec.Operation) string {
	if strings.HasSuffix(op.Path, ".json") {
		return manifest.RespJSON
	}
	return manifest.RespBinary
}

// bodySpec builds the body description for a POST operation. The body is
// required as soon as any field is mandatory; passing extra fields through is
// always allowed, so this never blocks a valid request.
func bodySpec(op spec.Operation) *manifest.Body {
	if op.Method != "POST" {
		return nil
	}
	b := &manifest.Body{Fields: params(op.Params)}
	for _, p := range op.Params {
		if p.Required {
			b.Required = true
			break
		}
	}
	b.Example = example(op.Params)
	return b
}

// example renders a JSON skeleton of the mandatory fields, the smallest body
// the docs say the endpoint accepts. Optional fields are left out to keep the
// help readable; the field table above it lists them all.
func example(ps []spec.Param) string {
	obj := exampleObject(ps)
	if len(obj) == 0 {
		return ""
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(raw)
}

func exampleObject(ps []spec.Param) map[string]any {
	obj := map[string]any{}
	for _, p := range ps {
		if !p.Required {
			continue
		}
		obj[p.Name] = exampleValue(p)
	}
	return obj
}

func exampleValue(p spec.Param) any {
	if len(p.Sub) > 0 {
		inner := exampleObject(p.Sub)
		// The docs phrase nested structures as "This is a JSON array [{...}]"
		// or as a single JSON object; mirror what they say.
		if strings.Contains(p.Doc, "JSON array") {
			return []any{inner}
		}
		return inner
	}
	switch p.Type {
	case "NUMBER":
		return 0
	case "BOOLEAN":
		return false
	case "CSV":
		return "1,2,3"
	case "DATE":
		return "2026-01-01"
	default:
		if len(p.Values) > 0 {
			return p.Values[0]
		}
		return ""
	}
}
