package main

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/spec"
	"golang.org/x/net/html"
)

// apiPrefix is what every documented endpoint path starts with. The spec
// stores paths relative to it, because the CLI joins them onto a per-org base
// URL that already ends in /api/v1.
const apiPrefix = "/api/v1"

// Parse extracts the API surface from the CashCtrl help page. The page is
// generated, so its structure is stable: one <section class="action"> per
// endpoint, holding a breadcrumb group, an <h3> title, a description, a
// parameter table, and a <div class="endpoint"> with "METHOD /api/v1/...".
func Parse(r io.Reader, source string) (*spec.Spec, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var ops []spec.Operation
	var problems []string
	for _, section := range findAll(doc, isSection) {
		// Prose sections (intro, auth, errors) reuse the endpoint style for
		// things like the base URL, so only a div whose text is "METHOD /api/v1/…"
		// names an operation. A section with none is prose; more than one means
		// the page changed shape and the parser needs to be looked at.
		var endpoints []*html.Node
		for _, div := range findAll(section, isClass("div", "endpoint")) {
			if f := strings.Fields(text(div)); len(f) == 2 && f[0] == strings.ToUpper(f[0]) && strings.HasPrefix(f[1], "/") {
				endpoints = append(endpoints, div)
			}
		}
		if len(endpoints) == 0 {
			continue
		}
		if len(endpoints) > 1 {
			problems = append(problems, fmt.Sprintf("section %q documents %d endpoints, want 1", anchorOf(section), len(endpoints)))
			continue
		}
		op, err := parseSection(section, endpoints[0])
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		ops = append(ops, *op)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("parse problems:\n  %s", strings.Join(problems, "\n  "))
	}

	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Method < ops[j].Method
	})
	return &spec.Spec{SpecVersion: spec.Version, Source: source, Operations: ops}, nil
}

func parseSection(section, endpoint *html.Node) (*spec.Operation, error) {
	fields := strings.Fields(text(endpoint))
	if len(fields) != 2 {
		return nil, fmt.Errorf("section %q: endpoint %q is not METHOD PATH", anchorOf(section), text(endpoint))
	}
	method, path := fields[0], fields[1]
	if method != "GET" && method != "POST" {
		return nil, fmt.Errorf("%s: unsupported method %q", path, method)
	}
	if !strings.HasPrefix(path, apiPrefix+"/") {
		return nil, fmt.Errorf("%s %s: path outside %s", method, path, apiPrefix)
	}
	path = strings.TrimPrefix(path, apiPrefix)

	op := &spec.Operation{Method: method, Path: path}
	if bc := findFirst(section, isClass("div", "breadcrumbs")); bc != nil {
		op.Group = strings.Join(strings.Fields(text(bc)), " ")
	}
	if h3 := findFirst(section, isElement("h3")); h3 != nil {
		op.Title = normalize(text(h3))
	}
	if desc := findFirst(section, isClass("div", "description")); desc != nil {
		var paras []string
		for _, p := range findAll(desc, isElement("p")) {
			if t := normalize(text(p)); t != "" {
				paras = append(paras, t)
			}
		}
		op.Doc = strings.Join(paras, " ")
	}

	// The outer parameter table only: nested "parameters sub" tables belong to
	// the row that holds them and are parsed recursively by parseRows.
	for _, tbl := range findAll(section, isClass("table", "parameters")) {
		if hasClass(tbl, "sub") {
			continue
		}
		op.Params = append(op.Params, parseRows(tbl)...)
	}
	if op.Title == "" {
		return nil, fmt.Errorf("%s %s: no title", method, path)
	}
	return op, nil
}

// parseRows parses the direct rows of one parameter table. Rows of nested
// sub-tables are reached only through their parent row's recursion, never
// flattened into the outer list.
func parseRows(table *html.Node) []spec.Param {
	var params []spec.Param
	for _, tr := range directRows(table) {
		th := findFirst(tr, isElement("th"))
		td := findFirst(tr, isElement("td"))
		if th == nil || td == nil {
			continue // header or layout row
		}
		p := spec.Param{Name: normalize(text(th))}
		for _, lbl := range findAll(td, isClass("div", "label")) {
			// Labels inside a nested sub-table describe the sub-params, not
			// this row; without this guard the last sub-param's datatype and
			// any sub-param's "mandatory" would leak onto the parent.
			if closestTable(lbl) != closestTable(td) {
				continue
			}
			t := normalize(text(lbl))
			switch {
			case t == "mandatory":
				p.Required = true
			case hasClass(lbl, "datatype"):
				p.Type = t
			}
		}
		var paras []string
		for _, para := range findAll(td, isElement("p")) {
			// Paragraphs inside a nested sub-table document the sub-params.
			if closestTable(para) != closestTable(td) {
				continue
			}
			if t := normalize(text(para)); t != "" {
				paras = append(paras, t)
			}
		}
		p.Doc, p.Values = extractValues(strings.Join(paras, " "))
		if sub := findFirst(td, isClass("table", "sub")); sub != nil {
			p.Sub = parseRows(sub)
		}
		if p.Name != "" {
			params = append(params, p)
		}
	}
	return params
}

// possibleValues matches the docs' fixed phrasing for enumerations, e.g.
// "Possible values: MAIN, INVOICE, DELIVERY." — always a trailing sentence.
var possibleValues = regexp.MustCompile(`\s*Possible values:\s*([^.]+)\.?\s*$`)

// extractValues splits a doc string into prose and the enumerated values the
// docs append as a "Possible values" sentence, so the manifest can carry them
// as data instead of the CLI re-parsing prose at runtime.
func extractValues(doc string) (string, []string) {
	m := possibleValues.FindStringSubmatch(doc)
	if m == nil {
		return doc, nil
	}
	var values []string
	for _, v := range strings.Split(m[1], ",") {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return doc, nil
	}
	return strings.TrimSpace(strings.TrimSuffix(doc, m[0])), values
}

// Validate refuses a spec that is too small or self-contradictory to ship:
// the vendored file is the build's single source of truth, so a bad
// extraction must fail here rather than generate a wrong manifest.
func Validate(s *spec.Spec, minOps int) error {
	if len(s.Operations) < minOps {
		return fmt.Errorf("only %d operations extracted, want at least %d", len(s.Operations), minOps)
	}
	seen := map[string]bool{}
	for _, op := range s.Operations {
		key := op.Method + " " + op.Path
		if seen[key] {
			return fmt.Errorf("duplicate operation %s", key)
		}
		seen[key] = true
		if op.Method == "GET" {
			continue
		}
		if op.Method != "POST" {
			return fmt.Errorf("%s: unsupported method", key)
		}
	}
	return nil
}

// --- small DOM helpers -----------------------------------------------------

func isElement(name string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == name }
}

func isSection(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Data == "section" && hasClass(n, "action")
}

func isClass(name, class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == name && hasClass(n, class)
	}
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// anchorOf names a section in error messages: its first <a id=...>.
func anchorOf(section *html.Node) string {
	for _, a := range findAll(section, isElement("a")) {
		if id := attr(a, "id"); id != "" {
			return id
		}
	}
	return "?"
}

// findAll returns every node under root (excluding root) matching pred, in
// document order.
func findAll(root *html.Node, pred func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if pred(c) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(root)
	return out
}

func findFirst(root *html.Node, pred func(*html.Node) bool) *html.Node {
	if all := findAll(root, pred); len(all) > 0 {
		return all[0]
	}
	return nil
}

// directRows returns the tr nodes of table itself, not of nested tables.
func directRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "table" {
				continue
			}
			if c.Type == html.ElementNode && c.Data == "tr" {
				rows = append(rows, c)
				continue // cells may hold nested tables; their rows are not ours
			}
			walk(c)
		}
	}
	walk(table)
	return rows
}

// closestTable returns the nearest ancestor table of n (or n itself).
func closestTable(n *html.Node) *html.Node {
	for ; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && n.Data == "table" {
			return n
		}
	}
	return nil
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }
