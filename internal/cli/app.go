// Package cli builds the cashctrl command tree from the embedded manifest and
// turns process arguments into exactly one API call (or one merged page walk).
//
// Two rules shape everything here: stdout carries the API response and nothing
// else, and every failure leaves as a single line of JSON on stderr so an
// agent can parse it without heuristics.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wir-drei-digital/cash-ctrl-cli/internal/api"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/config"
	"github.com/wir-drei-digital/cash-ctrl-cli/internal/manifest"
)

type app struct {
	manifest       *manifest.Manifest
	client         *api.Client
	res            config.Resolved
	stdout, stderr io.Writer
	stdin          io.Reader
	// prompt and isTerminal exist for `init`, the one interactive command.
	// Both nil in production, where init substitutes the real terminal; tests
	// fill them to drive the wizard without one. Nothing else reads them.
	prompt     prompter
	isTerminal func() bool
}

// Execute is the process entry point: it loads the embedded manifest and the
// resolved config, runs the command tree, and returns the exit code
// (0 success, 1 API/network failure, 2 usage error).
func Execute(args []string) int {
	m, err := manifest.Load()
	if err != nil {
		// The manifest is embedded, so this is a broken binary rather than
		// anything the caller did — but it still leaves as parseable JSON.
		a := &app{stderr: os.Stderr}
		return a.renderError(api.Usagef("%v: this build's embedded manifest is unreadable, reinstall cashctrl", err))
	}
	res, err := config.Resolve(os.Getenv)
	if err != nil {
		a := &app{stderr: os.Stderr}
		return a.renderError(api.Usagef("%v", err))
	}

	// Ctrl-C and SIGTERM cancel the request in flight *and* any retry wait:
	// the client's Sleep hook is the only way to interrupt a 429 backoff,
	// which can otherwise run tens of seconds past the signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &app{
		manifest: m,
		res:      res,
		client: &api.Client{
			BaseURL: res.BaseURL, APIKey: res.APIKey, Lang: res.Lang,
			ReadOnly: res.ReadOnly, AllowCustomBase: res.AllowCustomBase,
			Sleep: contextSleeper(ctx),
		},
		stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin,
	}
	return a.runContext(ctx, args)
}

// contextSleeper returns a sleep function that gives up as soon as ctx is done,
// so an interrupted run does not sit out the remainder of a retry backoff.
func contextSleeper(ctx context.Context) func(time.Duration) {
	return func(d time.Duration) {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
}

func (a *app) run(args []string) int { return a.runContext(context.Background(), args) }

func (a *app) runContext(ctx context.Context, args []string) int {
	root := a.newRoot()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		return a.renderError(err)
	}
	return 0
}

// renderError writes err as one line of JSON on stderr and returns the exit
// code: 2 for usage errors (the caller can fix the command), 1 for everything
// the API or the network produced.
func (a *app) renderError(err error) int {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		// Anything not from the api package is a cobra parse/usage failure.
		apiErr = api.Usagef("%v", err)
	}
	raw, mErr := json.Marshal(apiErr)
	if mErr != nil {
		// Details came from the server and may not be marshalable; never lose
		// the error itself over it.
		raw, mErr = json.Marshal(&api.Error{Kind: apiErr.Kind, Message: apiErr.Message, Status: apiErr.Status})
		if mErr != nil {
			raw = []byte(`{"error":"internal: cannot render error","kind":"usage","details":null}`)
		}
	}
	fmt.Fprintln(a.stderr, string(raw))
	if apiErr.Kind == api.KindUsage {
		return 2
	}
	return 1
}
