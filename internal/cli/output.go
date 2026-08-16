package cli

import (
	"mime"
	"os"
	"strings"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// writeResponse puts the response body on stdout — or in outputPath — byte for
// byte. The single transformation is a trailing newline on textual payloads
// that lack one, so shell output stays readable; binary payloads are never
// touched.
func (a *app) writeResponse(op *manifest.Operation, resp *api.Response, outputPath string) error {
	if len(resp.Body) == 0 {
		return nil
	}
	if outputPath != "" {
		if err := os.WriteFile(outputPath, resp.Body, 0o644); err != nil {
			return api.Usagef("writing %s: %v", outputPath, err)
		}
		return nil
	}
	a.stdout.Write(resp.Body)
	if !isBinary(op, resp.Header.Get("Content-Type")) && resp.Body[len(resp.Body)-1] != '\n' {
		a.stdout.Write([]byte("\n"))
	}
	return nil
}

// isBinary decides whether a payload may be appended to. The manifest is the
// primary signal; the response's own content type is the fallback, because
// servers happily label a JSON array "text/plain".
func isBinary(op *manifest.Operation, contentType string) bool {
	if op.Response == manifest.RespBinary {
		return true
	}
	if contentType == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = contentType
	}
	switch {
	case strings.HasPrefix(mt, "text/"),
		strings.Contains(mt, "json"),
		strings.Contains(mt, "xml"):
		return false
	}
	return true
}
