# cashctrl

A command-line client for the [CashCtrl](https://cashctrl.com) API, built for agents and
scripts: JSON in, JSON out, one static binary, no runtime dependencies. The command tree is
generated from CashCtrl's API reference, so all 376 documented endpoints — accounts, journal,
orders (invoices/quotes), people, inventory, files, fiscal periods, reports, salary — are
reachable as `cashctrl <resource> <verb>` without hand-written glue. stdout carries the API
response and nothing else; every failure leaves one line of JSON on stderr.

CashCtrl publishes no OpenAPI document, so the spec is extracted from its generated help page
([api docs](https://app.cashctrl.com/static/help/en/api/index.html)), vendored under `spec/`,
and compiled into the binary. `cashctrl api <METHOD> <path>` reaches anything the generated
tree might miss.

## Install

```sh
go install github.com/wir-drei-digital/cash-ctrl-cli/cmd/cashctrl@latest
```

Or download a prebuilt binary from [GitHub Releases](https://github.com/wir-drei-digital/cash-ctrl-cli/releases):

| OS      | Architectures | Archive  |
| ------- | ------------- | -------- |
| Linux   | amd64, arm64  | `.tar.gz` |
| macOS   | amd64, arm64  | `.tar.gz` |
| Windows | amd64         | `.zip`   |

Each release also ships `checksums.txt`. Unpack the archive and put `cashctrl` on your `PATH`.

## Authentication

CashCtrl authenticates with an **API key** over HTTP Basic auth (the key is the username, the
password stays empty). The API is available on the PRO plan, and a key belongs to exactly one
organization: create an API user under *Settings > Users & Roles > Add > Add API user* in the
organization you want to reach — its role decides what the key may do. To experiment safely,
clone your organization into a test org (*Settings > Organizations > Copy*) and create the API
user there.

The CLI therefore needs two values: the **organization subdomain** (the `myorg` in
`https://myorg.cashctrl.com`) and the **API key**. The quickest way in is the wizard:

```sh
cashctrl init
```

It asks for both, stores them with `0600` permissions, and proves they work with one read-only
API call. It needs a terminal; the rest of this section is the non-interactive path.

Either export both values:

```sh
export CASHCTRL_ORG=myorg
export CASHCTRL_API_KEY=...
```

or store them in the config file (the key is read from stdin, never from argv):

```sh
cashctrl config set org myorg
echo $KEY | cashctrl config set api-key
```

The config file lives at `<user config dir>/cashctrl/config.json` (`cashctrl config path`
prints the exact location) and is written with `0600` permissions. Environment variables always
win over the file. `cashctrl config unset <key>` removes a stored value again.

`cashctrl auth status` prints what is active as one line of JSON, never the key itself: `mode`
is `key` or `none`, `key_source`/`org_source` say where each value came from (`env` or
`config`), and a configuration that cannot authenticate — a key without an org, say — carries a
`hint` naming the missing piece. `cashctrl auth verify` proves the credentials against the real
API with one read-only call (`GET /currency/list.json`).

> **The API key is as powerful as the API user's role.** It does not expire on its own, and
> anyone holding it can act as that user. Treat it like a password: never commit it, never
> paste it into a command line, and give the API user the least role that does the job. See
> [SECURITY.md](SECURITY.md).

## Usage

```sh
# Discover: every command, machine-readable (schema_version 1)
cashctrl commands --json

# Read — query parameters are flags
cashctrl person list --limit 10 --only-customers
cashctrl person read --id 42

# Create — the body is JSON via --data (literal, @file, or - for stdin)
cashctrl person create --data '{"firstName":"Maria","lastName":"Muster"}'

# Nested structures are plain JSON; the CLI translates to CashCtrl's dialect
cashctrl journal create --data '{"dateAdded":"2026-08-16","title":"Entry","debitId":1,"creditId":15,"amount":500}'

# Delete-class operation: refuses to run without --force
cashctrl person delete --data '{"ids":"42"}' --force

# Walk every page and emit one merged JSON array
cashctrl journal list --all

# Downloads: exports and documents are separate commands, named by format
cashctrl person list-xlsx --output people.xlsx
cashctrl report collection download-annualreport-pdf --output report.pdf
cashctrl file get --id 7 --output invoice.pdf

# Upload a local file (prepare → put → persist, composed by the CLI)
cashctrl file upload ./receipt.pdf

# Escape hatch for anything the generated tree does not cover
cashctrl api GET /person/list.json --query limit=5
```

**How requests translate.** The CashCtrl API takes GET query parameters and form-encoded POST
bodies, with nested structures embedded as JSON strings inside single form fields. The CLI
keeps the agent side uniform — GET parameters are flags, POST bodies are one JSON object via
`--data` — and translates mechanically: scalars go verbatim, `true`/`false` and numbers as
their literal text, arrays and objects as compact JSON, `null` values are dropped. A field the
field table does not list still reaches the API. `cashctrl <resource> <verb> --help` prints the
field table (nested fields included) and an example body for every body-taking operation.

**Formats.** Endpoints that exist in several output formats are separate commands named by
extension: `person list` (JSON) has `list-csv`, `list-pdf`, `list-xlsx`, `list-vcf` siblings,
documents have `read-pdf`/`read-zip`, report elements `download-pdf/csv/xlsx/xml`. Non-JSON
responses are passed through byte for byte; `--output <file>` is the tidy way to take one.

**Language.** `--lang de|en|fr|it` (or `CASHCTRL_LANG`, or `cashctrl config set lang`) selects
the language of error messages and generated documents.

**Pagination.** List endpoints page with `start`/`limit` and answer `{"total", "data"}`;
`--all` walks every page and emits one merged JSON array of the items. HTTP 429 responses are
retried automatically with growing waits that honour `Retry-After`, within a total budget; do
not add your own retry loop on top. Ctrl-C (or SIGTERM) cancels the request in flight *and*
any retry wait it is sitting in.

## Guardrails

- **`--force` for destructive work.** Every delete-class (45) and send-class (3) operation
  refuses to run without `--force`. The check happens before any network I/O, so an
  unconfirmed command has no side effects at all. Send-class means e-mail leaves for a real
  recipient (`order document mail`, `salary document mail`, `salary certificate document
  mail`); delete-class includes `file empty-archive`, which purges the recycle bin for good.
- **`CASHCTRL_READ_ONLY=1`** blocks every operation that is not read-class — every POST, since
  CashCtrl mutates only via POST. `cashctrl api` is stricter under it and allows only GET.
  `cashctrl config set read-only true` persists the same setting.
- **Custom-base lockout.** The API key is only ever sent to `*.cashctrl.com` over HTTPS.
  Pointing `CASHCTRL_API_BASE` somewhere else is refused unless you also set
  `CASHCTRL_ALLOW_CUSTOM_BASE=1`, which exists for tests and proxies and is a deliberate
  opt-in to sending your credentials elsewhere.
- **Redirects.** GET downloads follow redirects (CashCtrl hands file contents off to its
  storage provider that way) — but only to HTTPS targets, and Go strips the Authorization
  header the moment the redirect leaves the API host. A redirected POST is never followed:
  replaying a mutation body is the one thing the retry policy is built to never do.
- **`cashctrl api` is not a bypass.** A path the manifest recognises inherits that operation's
  risk class; a POST it cannot classify is treated as the dangerous option and needs
  `--force`; a POST to a `*/delete.json` or `*/empty_archive.json` path always needs
  `--force`.

Environment variables: `CASHCTRL_API_KEY`, `CASHCTRL_ORG`, `CASHCTRL_API_BASE`,
`CASHCTRL_LANG`, `CASHCTRL_READ_ONLY`, `CASHCTRL_ALLOW_CUSTOM_BASE`. Where the same setting
also lives in the config file, the environment wins.

## Error contract & exit codes

Errors are one line of JSON on stderr — never on stdout, which stays reserved for the response:

```json
{"kind":"validation","error":"POST /person/create.json: the API reports success=false (lastName: Value required.)","status":200,"details":{"success":false,"errors":[{"field":"lastName","message":"Value required."}]}}
```

`status` is omitted when no HTTP response was involved; `details` carries the parsed API error
body when there is one, and is `null` otherwise. Branch on `kind`:

| `kind`            | Meaning                                                                |
| ----------------- | ---------------------------------------------------------------------- |
| `auth`            | 401 — API key missing or wrong                                         |
| `forbidden`       | 403 — the API user's role lacks permission; adjust it under Settings > Users & Roles |
| `not_found`       | 404 or 410                                                             |
| `validation`      | the request was rejected — including CashCtrl's in-band form validation, which answers **HTTP 200 with `"success": false`**: the CLI turns that into this error (with `status: 200` and the body in `details`) so a rejected write can never exit 0. Also other 4xx, and a refused redirect |
| `rate_limited`    | 429 and the retry budget was exhausted                                 |
| `server`          | 5xx                                                                    |
| `transport`       | network failure that is safe to retry: the request never left, or it was a read |
| `outcome_unknown` | the request may or may not have been applied — verify before retrying   |
| `incomplete`      | `--all` hit `--max-pages`; stdout holds a partial but real result       |
| `usage`           | the command itself was wrong; nothing was sent                         |

| Exit code | Meaning                                                    |
| --------- | ---------------------------------------------------------- |
| `0`       | success                                                    |
| `1`       | API or network error (any `kind` except `usage`)           |
| `2`       | usage error — bad command, missing credentials, unconfirmed force gate, read-only violation |

One exit-2 case is not your fault: if the embedded manifest cannot be read, the binary reports
`kind:"usage"` with a message telling you to reinstall. The manifest is compiled into the
binary, so that only happens if the build itself is broken.

## Skiplisted operations

**None.** All 376 endpoints the API reference documents are generated into the CLI.

The generator supports excluding operations through the `skiplist` map in
`tools/genmanifest/overrides.json`, keyed by `"METHOD /path"` with a mandatory human-readable
reason. An entry with an empty reason fails generation, and so does an entry that matches no
operation in the current spec — a skiplist cannot silently rot across spec refreshes. Any
change to that table belongs in this section too: it is the list users read before assuming a
command exists.

## Development

```sh
make build        # go build -o bin/cashctrl ./cmd/cashctrl
make test         # go test ./...
make generate     # regenerate internal/manifest/manifest.json.gz from the vendored spec
make update-spec  # re-fetch spec/cashctrl-api.json from the CashCtrl docs (network)
make check        # go vet + manifest drift check + tests — what CI runs
```

`e2e/smoke_test.go` builds the real binary and drives it against a fake API server;
`e2e/live_test.go` is a read-only check against production, skipped unless you run it with
`CASHCTRL_LIVE=1 CASHCTRL_ORG=... CASHCTRL_API_KEY=... go test ./e2e -run TestLive -v`.

**Refreshing the spec.** CashCtrl publishes no OpenAPI document; `make update-spec` extracts
the API surface from the published help page, validates it (GET/POST only, no duplicates, at
least 350 operations) and refuses to write anything that fails those checks. The spec is
vendored, so a broken extraction can never break a build. The workflow:

1. `make update-spec` — rewrites `spec/cashctrl-api.json` and `spec/PROVENANCE.json`.
2. `make generate` — rebuilds the embedded manifest. New operations are *not* guessed into
   it: the generator prints them as unclassified and exits non-zero. Run
   `go run ./tools/genmanifest -write-proposals`, review every proposed risk class in
   `tools/genmanifest/classifications.proposed.json`, then rename it over
   `classifications.json` and run `make generate` again.
3. Review the diff of the generated command list — renamed or removed commands are breaking
   changes and must show up in the release notes. The generator's golden file
   (`tools/genmanifest/testdata/golden_commands.txt`) carries each operation's risk class,
   pagination and response kind, so a changed gate shows up there too; regenerate it with
   `UPDATE_GOLDEN=1 go test ./tools/genmanifest` only after reading the diff.
4. Commit the spec, the manifest, and the classification changes together.

**Compatibility.** This project follows SemVer. The public API is: command paths, flags, exit
codes, the stderr error schema, and the `cashctrl commands --json` catalog schema
(`schema_version`). Changes to any of those are breaking. Additive optional fields are the
exception: a new optional key may appear in the catalog under the same `schema_version`, so
consumers must ignore keys they do not know. The Go packages under `internal/` are not part of
the public API, and neither is the wording of human-readable messages.

## License

MIT — see [LICENSE](LICENSE).
