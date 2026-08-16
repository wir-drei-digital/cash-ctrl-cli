package cli

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

// maxUpload caps a single file so a mistaken path (a disk image, /dev/zero on
// some systems) fails locally instead of streaming gigabytes at the API.
const maxUpload = 100 << 20

// attachUpload hangs the hand-written `file upload` composite off the
// generated `file` group. It exists because CashCtrl has no single upload
// endpoint: uploading is prepare (get a presigned URL) → PUT the bytes to
// storage → persist, and the middle step is not an API call at all — without
// this command the CLI could not put a file into CashCtrl.
func (a *app) attachUpload(root *cobra.Command) {
	for _, sub := range root.Commands() {
		if sub.Name() == "file" {
			sub.AddCommand(a.uploadCommand())
			return
		}
	}
	// No file group would mean the manifest lost the file endpoints; the
	// composite depends on them, so it disappears along with them.
}

func (a *app) uploadCommand() *cobra.Command {
	var name, mimeType string
	var categoryID int
	cmd := &cobra.Command{
		Use:   "upload <path>",
		Short: "Upload a local file (prepare → put → persist) and print its file ID",
		Long: "Upload a local file to the CashCtrl file manager.\n\n" +
			"This is a composite of three steps — POST /file/prepare.json, an HTTP PUT of\n" +
			"the bytes to the presigned storage URL prepare returns, and POST\n" +
			"/file/persist.json — because CashCtrl has no single upload endpoint.\n\n" +
			"stdout carries one line of JSON composed by the CLI (not a raw API\n" +
			"response): {\"file_id\": n, \"name\": \"...\", \"mime_type\": \"...\"}.\n" +
			"Attach the ID to entries via their update endpoints, or set metadata with\n" +
			"`cashctrl file update`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			info, err := os.Stat(path)
			if err != nil {
				return api.Usagef("%v", err)
			}
			if info.Size() > maxUpload {
				return api.Usagef("%s is %d bytes; the CLI caps uploads at %d", path, info.Size(), maxUpload)
			}
			if name == "" {
				name = filepath.Base(path)
			}
			if mimeType == "" {
				mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
				if mimeType == "" {
					mimeType = "application/octet-stream"
				}
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return api.Usagef("%v", err)
			}
			if err := a.applyGlobalFlags(cmd); err != nil {
				return err
			}

			// Step 1: prepare — announce name and mime type, get the
			// presigned write URL and the (still temporary) file ID.
			fileMeta := map[string]any{"name": name, "mimeType": mimeType, "size": info.Size()}
			if categoryID != 0 {
				fileMeta["categoryId"] = categoryID
			}
			files, err := json.Marshal([]any{fileMeta})
			if err != nil {
				return api.Usagef("%v", err)
			}
			prepResp, err := a.client.Do(cmd.Context(), api.Request{
				Method: "POST", Path: "/file/prepare.json",
				Form: url.Values{"files": {string(files)}}, Risk: manifest.RiskWrite,
			})
			if err != nil {
				return err
			}
			var prep struct {
				Data []struct {
					FileID   json.Number `json:"fileId"`
					WriteURL string      `json:"writeUrl"`
				} `json:"data"`
			}
			if err := json.Unmarshal(prepResp.Body, &prep); err != nil || len(prep.Data) != 1 || prep.Data[0].WriteURL == "" {
				return &api.Error{Kind: api.KindServer,
					Message: "file prepare did not return a write URL", Details: string(prepResp.Body)}
			}

			// Step 2: put — the bytes go to storage, not to the API host. The
			// URL is presigned, so no credential travels with them.
			if err := a.client.PutFile(cmd.Context(), prep.Data[0].WriteURL, mimeType, body); err != nil {
				return err
			}

			// Step 3: persist — without it the upload is deleted within hours.
			if _, err := a.client.Do(cmd.Context(), api.Request{
				Method: "POST", Path: "/file/persist.json",
				Form: url.Values{"ids": {prep.Data[0].FileID.String()}}, Risk: manifest.RiskWrite,
			}); err != nil {
				return err
			}

			id, _ := strconv.ParseInt(prep.Data[0].FileID.String(), 10, 64)
			out, err := json.Marshal(struct {
				FileID   int64  `json:"file_id"`
				Name     string `json:"name"`
				MimeType string `json:"mime_type"`
			}{id, name, mimeType})
			if err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintln(a.stdout, string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "file name in CashCtrl (default: the local base name)")
	cmd.Flags().StringVar(&mimeType, "mime", "", "mime type (default: derived from the extension)")
	cmd.Flags().IntVar(&categoryID, "category-id", 0, "file category ID")
	return cmd
}
