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

// DeliveryVerifyDelay is the delay before checking whether text is still visible
// in the prompt after a send.
var DeliveryVerifyDelay = 20 * time.Second

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

// VerifyDelivery checks whether the beginning of the sent text is still visible
// on the bottom line after the prompt should have been submitted. If it is, the
// message is re-sent by clearing the prompt, typing again, and pressing Enter.
// The returned bool reports whether a retry was performed.
func VerifyDelivery(adapter Adapter, session, sentText string) (bool, error) {
	time.Sleep(DeliveryVerifyDelay)

	screen, err := adapter.DumpScreen(session)
	if err != nil {
		return false, fmt.Errorf("dump-screen failed: %w", err)
	}

	if !HasDeliveryPending(screen, sentText) {
		return false, nil
	}

	if err := adapter.ClearInput(session); err != nil {
		return true, fmt.Errorf("clear input failed: %w", err)
	}
	if err := adapter.TypeText(session, sentText); err != nil {
		return true, fmt.Errorf("retype text failed: %w", err)
	}
	if err := adapter.SendEnter(session); err != nil {
		return true, fmt.Errorf("retry send-enter failed: %w", err)
	}
	if err := VerifySend(adapter, session, sentText); err != nil {
		return true, fmt.Errorf("post-retry verification failed: %w", err)
	}

	return true, nil
}

// HasDeliveryPending reports whether the first part of sentText still appears
// on the last non-empty line of the screen dump.
func HasDeliveryPending(screenDump, sentText string) bool {
	prefix := deliveryPrefix(sentText)
	if prefix == "" {
		return false
	}

	lines := strings.Split(screenDump, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		return strings.Contains(line, prefix)
	}

	return false
}

func deliveryPrefix(sentText string) string {
	text := strings.TrimSpace(sentText)
	if text == "" {
		return ""
	}

	runes := []rune(text)
	if len(runes) > 20 {
		runes = runes[:20]
	}
	return string(runes)
}
