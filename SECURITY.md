# Security

This document describes what the `cashctrl` CLI does with your credentials, what it refuses to
do, and what remains your responsibility.

## What the API key is

A CashCtrl API key authenticates as an **API user** — a real user account in one organization,
with the permissions of the role you assigned it. The key:

- can do everything that role allows, on all of that organization's data;
- does not expire on its own;
- travels with **every** request (HTTP Basic auth, key as username, empty password);
- can only be revoked by deleting or re-keying the API user under *Settings > Users & Roles*.

Two consequences. First, **least privilege lives in the role**: the CLI cannot narrow what a
key may do, so give the API user the smallest role that does the job, and use a separate API
user per integration so one can be revoked without breaking the others. Second, treat the key
like a password — anyone holding it is that user.

## How the CLI handles the key

- **Never from argv.** `cashctrl config set api-key` reads the key from stdin only; passing it
  as an argument is an error. Command lines are visible to every process on the machine
  (`ps`), end up in shell history, and leak through CI logs.
- **On disk with `0600`.** The config file (`cashctrl config path`) is written with `0600`
  permissions inside a `0700` directory, via temp-file-and-rename so a concurrent reader never
  sees a partial file. On shared machines, prefer the `CASHCTRL_API_KEY` environment variable
  over the file.
- **Never printed.** `cashctrl auth status` reports *whether* and *from where* a key is
  configured, never the key. `--verbose` logs method, path and status — the Authorization
  header is never written to any log the CLI produces.
- **Only to CashCtrl, only over HTTPS.** The key is sent exclusively to `*.cashctrl.com` over
  HTTPS. Overriding the base URL (`CASHCTRL_API_BASE`) toward any other host is refused unless
  `CASHCTRL_ALLOW_CUSTOM_BASE=1` is also set — an explicit, deliberate opt-in that exists for
  tests and proxies. Plain HTTP is refused everywhere except loopback.
- **Redirects cannot steal it.** GET downloads follow redirects because CashCtrl serves file
  contents from a storage provider that way — but only to HTTPS targets, and the
  Authorization header is stripped as soon as the redirect leaves the API host (Go's own
  cross-host rule). POSTs never follow a redirect at all: a 307/308 would replay the mutation
  body, and mutations are never replayed. Presigned upload URLs (`file upload`) carry no
  credential in headers; the URL itself is the authorization, and the CLI refuses to PUT to a
  non-HTTPS one.

## Guardrails for unattended use

The CLI is built to be driven by agents, which means assuming the driver occasionally does
something it should not:

- **Destructive operations are gated.** Every delete-class and send-class command (sending
  e-mail to customers, emptying the file recycle bin) refuses to run without `--force`, and
  the refusal happens before any network I/O. `--force` is deliberately not settable through
  an environment variable: if an agent should delete or send, the flag has to appear per call,
  visible in the transcript.
- **Read-only mode.** `CASHCTRL_READ_ONLY=1` blocks every POST at the client, before the
  network. Give an agent that environment by default and lift it only for runs that are meant
  to write. Note this is a client-side guardrail, not a server-side one — for hard guarantees,
  assign the API user a read-only role in CashCtrl itself.
- **The escape hatch keeps the gates.** `cashctrl api` inherits the manifest's risk class for
  known paths, treats unknown POSTs as dangerous (`--force`), and permits only GET under
  read-only mode.
- **In-band failures are failures.** CashCtrl reports form validation errors as HTTP 200 with
  `"success": false`; the CLI converts that to a non-zero exit so automation can never
  mistake a rejected write for a success.

## Your responsibilities

- Test against a **cloned test organization** (*Settings > Organizations > Copy*), not your
  production books.
- Rotate the key by re-keying the API user if it may have leaked; delete API users that are no
  longer used.
- Remember the config file holds a long-lived credential: the machine's disk (and its backups)
  now hold access to your accounting. Full-disk encryption is your friend.
- The `lang`, `org` and base-URL settings are not secrets, but the org name appears in URLs —
  don't paste `--verbose` output anywhere you would not name your organization.

## Reporting a vulnerability

Please open a GitHub security advisory on this repository (Security > Advisories > Report a
vulnerability), or a private issue if advisories are unavailable to you. Do not open a public
issue with exploit details.
