package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// prompter is init's seam for interactive input. Production uses the real
// terminal; tests inject a script so the wizard runs without one.
type prompter interface {
	// Ask prints the prompt and reads one visible line.
	Ask(prompt string) (string, error)
	// AskSecret prints the prompt and reads one line without echo.
	AskSecret(prompt string) (string, error)
}

// terminalPrompter drives the real terminal. Prompts go to stderr so stdout
// keeps its contract (machine-readable results only).
type terminalPrompter struct{}

func (terminalPrompter) Ask(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("stdin closed")
	}
	return strings.TrimSpace(sc.Text()), nil
}

func (terminalPrompter) AskSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// stdinIsTerminal reports whether the process has an interactive terminal on
// stdin — the gate for `init`, the one interactive command.
func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
