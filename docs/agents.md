# cashctrl CLI — agent reference

Paste this block into your agent's instructions. It is the whole contract.

```markdown
# cashctrl CLI — agent reference

Auth: env vars CASHCTRL_ORG (organization subdomain) and CASHCTRL_API_KEY (CashCtrl API key),
or values a human stored with `cashctrl config set`. The environment wins whenever it is set;
with neither, every API command exits 2 with kind "usage".
- `cashctrl auth status` — the cheap credential-health check: one line of JSON, no network, no
  key in it. {"mode","org","key_set","key_source","org_source","hint"}; mode is "key" or
  "none". Read it before diagnosing an auth failure: a `hint` names exactly the missing piece
  (key without org, org without key, nothing at all). `hint` is absent from a healthy status.
- `cashctrl auth verify` — proves the credentials with one read-only call
  (GET /currency/list.json); {"ok":true,"org":...} on success, the normal error contract
  otherwise. kind "auth" (401) means the key is wrong; kind "forbidden" (403) means the API
  user's role lacks permission — that is fixed by a human in CashCtrl under Settings > Users &
  Roles, not by retrying and not by re-authenticating.
- `cashctrl init` is interactive and needs a terminal; never run it. Without a terminal it
  exits 2 with kind "usage". Configure credentials with the env vars instead.
- The API key belongs to exactly one organization. To work with several organizations, each
  needs its own key (and its own CASHCTRL_ORG value).

Discovery:
- `cashctrl commands --json` — full machine-readable catalog (schema_version 1): command,
  method, path, group, risk, summary, doc, pagination, response, query params, body fields
  (nested sub-fields included) and an example body.
- `cashctrl <resource> <verb> --help` — the same field table and example, human-readable.
- Never guess a command path; read it from the catalog.
- Endpoints that exist in several output formats are separate commands named by extension:
  `person list` (JSON) vs `person list-csv` / `list-pdf` / `list-xlsx`; `"response":"binary"`
  in the catalog marks the downloads.

Calls:
- GET parameters are flags in kebab-case (`--category-id 3` for categoryId; the API name from
  the catalog is authoritative). POST bodies are ONE JSON object via `--data '{...}'`,
  `--data @file.json`, or `--data -` (stdin).
- The CLI translates the JSON body to CashCtrl's form encoding: scalars verbatim, nested
  arrays/objects as embedded JSON, null values dropped. Write plain JSON; never pre-encode.
- IDs usually travel as parameters, not paths: `cashctrl person read --id 42`, and deletes
  take a CSV string of IDs in the body: `cashctrl person delete --data '{"ids":"1,2"}' --force`.
- stdout is the raw API response. List endpoints answer {"total": n, "data": [...]}; reads
  answer {"data": {...}}; writes answer {"success": true, "insertId": n, ...}.
- Errors are one line of JSON on stderr: {"kind","error","status","details"}. `status` is
  absent when no HTTP response was involved. Exit codes: 0 ok; 1 API/network error; 2 usage
  error. Branch on `kind`: auth, forbidden, not_found, validation, rate_limited, server,
  transport, outcome_unknown, incomplete, usage.
- CashCtrl reports form validation as HTTP 200 with "success": false; the CLI converts that
  to kind "validation" with status 200 and exit 1, with the server body in `details`. So exit
  0 really means the write was accepted.
- `kind:"outcome_unknown"` means the write may or may not have landed — verify state before
  retrying. `kind:"transport"` is safe to retry: the request never left, or it was a read.
- `--output <file>` writes the response body to a file (use it for the binary downloads).
- `--lang de|en|fr|it` selects the language of error messages and generated documents.
- `cashctrl file upload <path>` uploads a local file (CashCtrl's three-step flow, composed by
  the CLI) and prints {"file_id": n, "name": ..., "mime_type": ...} — CLI-composed, not a raw
  API response. Use the ID in `attachments` fields or `fileId` parameters elsewhere.

Safety:
- delete- and send-class commands refuse to run without `--force` (send = e-mail leaves for a
  real recipient: `order document mail`, `salary document mail`, `salary certificate document
  mail`). The refusal happens before any request is sent.
- CASHCTRL_READ_ONLY=1 blocks every POST; under it, `cashctrl api` accepts GET only.
- Lists: `--all` merges every page into one JSON array of the items (the envelope is
  dropped); exit 1 with kind "incomplete" means stdout holds a partial result (raise
  `--max-pages` or narrow the query).
- Unknown/new endpoints: `cashctrl api GET /x/y.json --query k=v` or
  `cashctrl api POST /x/y.json --data '{...}'` (paths relative to /api/v1; same guardrails;
  unclassified POSTs need `--force`).

Rate limits (429) are retried automatically; do not implement your own retry for 429.
```

## Notes for the human wiring this up

- The catalog is the source of truth for what exists. Regenerate the agent's tool list from
  `cashctrl commands --json` after upgrading the CLI rather than hard-coding command paths;
  `schema_version` tells you when the catalog shape itself changed.
- Grant the agent a read-only posture by default: `CASHCTRL_READ_ONLY=1` in its environment,
  lifted only for the runs that are meant to write. For a hard guarantee, also give the API
  user a read-only role in CashCtrl — the env var is a client-side gate.
- `--force` is deliberately not settable through an environment variable. If your agent should
  be able to delete or send, it has to pass the flag per call, which keeps the decision
  visible in the transcript.
- Never put the API key in the command line — the CLI only accepts it from `CASHCTRL_API_KEY`
  or from stdin via `cashctrl config set api-key`.
- Point the agent at a **cloned test organization** (*Settings > Organizations > Copy* in
  CashCtrl) while developing; the books it can reach are exactly as real as the org you hand
  it. The config file holds a long-lived credential — see [SECURITY.md](../SECURITY.md).
