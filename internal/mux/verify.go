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

var RestartPollInterval = 500 * time.Millisecond

var RestartTimeout = 20 * time.Second

type textLocator interface {
	LocateText(session string, text string) (string, error)
}

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

	lastLines := lastNonEmptyLines(screenDump, 5)
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

// VerifyTypedInputVisible confirms that text appeared in the focused pane before Enter.
// When zellij misroutes input to another pane, this returns a diagnostic error.
func VerifyTypedInputVisible(adapter Adapter, session, sentText string) error {
	time.Sleep(VerifyDelay)

	screen, err := adapter.DumpScreen(session)
	if err != nil {
		return fmt.Errorf("dump-screen failed: %w", err)
	}

	if HasTypedInputVisible(screen, sentText) {
		return nil
	}

	if locator, ok := adapter.(textLocator); ok {
		location, err := locator.LocateText(session, sentText)
		if err != nil {
			return fmt.Errorf("locating typed text across panes: %w", err)
		}
		if location != "" {
			return fmt.Errorf("typed text was not visible in the focused pane; found in %s", location)
		}
	}

	return fmt.Errorf("typed text was not visible in the focused pane")
}

// EnsureSessionReady restarts an exited agent inside the same mux session when the
// visible screen shows only a shell prompt and no agent UI.
func EnsureSessionReady(adapter Adapter, session, agent, launchCommand string) (bool, error) {
	screen, err := adapter.DumpScreen(session)
	if err != nil {
		return false, fmt.Errorf("dump-screen failed: %w", err)
	}

	if !LooksLikeShellPrompt(screen) || ScreenShowsAgentUI(screen) {
		return false, nil
	}
	if strings.TrimSpace(launchCommand) == "" {
		return false, fmt.Errorf("session is at a shell prompt but no restart command is available")
	}

	if err := adapter.TypeText(session, launchCommand); err != nil {
		return false, fmt.Errorf("typing restart command failed: %w", err)
	}
	if err := VerifyTypedInputVisible(adapter, session, launchCommand); err != nil {
		return false, fmt.Errorf("restart command routing check failed: %w", err)
	}
	if err := adapter.SendEnter(session); err != nil {
		return false, fmt.Errorf("sending restart command failed: %w", err)
	}
	if err := WaitForAgentReady(adapter, session, agent, RestartTimeout); err != nil {
		return false, err
	}
	return true, nil
}

func WaitForAgentReady(adapter Adapter, session, agent string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		screen, err := adapter.DumpScreen(session)
		if err == nil {
			if strings.EqualFold(agent, "codex") && strings.Contains(screen, "Do you trust the contents of this directory?") {
				if err := adapter.SendEnter(session); err != nil {
					return fmt.Errorf("accept codex trust prompt: %w", err)
				}
				time.Sleep(2 * time.Second)
				continue
			}
			if AgentReady(screen, agent) {
				return nil
			}
		}
		time.Sleep(RestartPollInterval)
	}

	return fmt.Errorf("timed out waiting for %s to become ready", agent)
}

func AgentReady(screen, agent string) bool {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "codex":
		return ScreenShowsCodexUI(screen)
	case "claude":
		return ScreenShowsClaudeUI(screen) || (strings.TrimSpace(screen) != "" && !LooksLikeShellPrompt(screen))
	default:
		return strings.TrimSpace(screen) != "" && !LooksLikeShellPrompt(screen)
	}
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

	for _, line := range reverseLines(screenDump) {
		line = strings.TrimSpace(line)
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

func HasTypedInputVisible(screenDump, sentText string) bool {
	if HasPendingInput(screenDump, sentText) {
		return true
	}

	prefix := deliveryPrefix(sentText)
	if prefix == "" {
		return false
	}

	for _, line := range lastNonEmptyLines(screenDump, 5) {
		if strings.Contains(line, prefix) {
			return true
		}
	}
	return false
}

func LooksLikeShellPrompt(screenDump string) bool {
	lines := lastNonEmptyLines(screenDump, 3)
	if len(lines) == 0 {
		return false
	}

	last := lines[len(lines)-1]
	for _, suffix := range []string{"$", "❯", "%"} {
		if strings.HasSuffix(last, suffix) {
			return true
		}
	}
	return false
}

func ScreenShowsAgentUI(screenDump string) bool {
	return ScreenShowsCodexUI(screenDump) || ScreenShowsClaudeUI(screenDump)
}

func ScreenShowsCodexUI(screenDump string) bool {
	return strings.Contains(screenDump, "OpenAI Codex") ||
		strings.Contains(screenDump, "/model to change") ||
		strings.Contains(screenDump, "esc to interrupt")
}

func ScreenShowsClaudeUI(screenDump string) bool {
	lower := strings.ToLower(screenDump)
	return strings.Contains(lower, "claude code") ||
		strings.Contains(lower, "bypassing permissions") ||
		strings.Contains(screenDump, "esc to interrupt")
}

func lastNonEmptyLines(screenDump string, limit int) []string {
	lines := strings.Split(screenDump, "\n")
	var lastLines []string
	for i := len(lines) - 1; i >= 0 && len(lastLines) < limit; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			lastLines = append([]string{trimmed}, lastLines...)
		}
	}
	return lastLines
}

func reverseLines(screenDump string) []string {
	lines := strings.Split(screenDump, "\n")
	reversed := make([]string, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		reversed = append(reversed, lines[i])
	}
	return reversed
}
