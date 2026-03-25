package mux

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// VerifyDelay is the delay between send and screen dump verification.
// Exposed as a variable for testing.
var VerifyDelay = 500 * time.Millisecond

const verifyMaxRetries = 3

// VerifySend checks that the sent text is no longer pending on the terminal
// input prompt. If the text still appears at the bottom of the screen (indicating
// Enter was not processed), it retries sending Enter up to verifyMaxRetries times.
func VerifySend(adapter Adapter, session, sentText string) error {
	for i := 0; i < verifyMaxRetries; i++ {
		time.Sleep(VerifyDelay)

		screen, err := adapter.DumpScreen(session)
		if err != nil {
			return fmt.Errorf("dump-screen failed: %w", err)
		}

		if !HasPendingInput(screen, sentText) {
			return nil
		}

		fmt.Fprintf(os.Stderr, "Text still pending on prompt, retrying Enter (%d/%d)...\n", i+1, verifyMaxRetries)

		if err := adapter.SendEnter(session); err != nil {
			return fmt.Errorf("retry send-enter failed: %w", err)
		}
	}

	// Final check after last retry
	time.Sleep(VerifyDelay)
	screen, err := adapter.DumpScreen(session)
	if err != nil {
		return fmt.Errorf("dump-screen failed: %w", err)
	}

	if HasPendingInput(screen, sentText) {
		return fmt.Errorf("text still pending on prompt after %d retries", verifyMaxRetries)
	}

	return nil
}

// HasPendingInput checks if sentText appears to still be on the terminal input
// prompt by examining the last non-empty lines of the screen dump. It uses a
// suffix match: if the bottom of the screen ends with the sent text, the text
// is likely still pending (Enter was not processed). After successful submission,
// new content (processing indicator, response, etc.) would appear below the text.
func HasPendingInput(screenDump, sentText string) bool {
	sentText = strings.TrimSpace(sentText)
	if sentText == "" {
		return false
	}

	lines := strings.Split(screenDump, "\n")

	// Collect last non-empty lines (preserving order) to handle text wrapping
	var lastLines []string
	for i := len(lines) - 1; i >= 0 && len(lastLines) < 5; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			lastLines = append([]string{trimmed}, lastLines...)
		}
	}

	if len(lastLines) == 0 {
		return false
	}

	// Join and trim to form a single string representing the bottom of the screen
	joined := strings.TrimSpace(strings.Join(lastLines, " "))

	// For long text, use a suffix to handle terminal width truncation
	checkText := sentText
	if len(checkText) > 50 {
		checkText = sentText[len(sentText)-50:]
	}

	return strings.HasSuffix(joined, checkText)
}
