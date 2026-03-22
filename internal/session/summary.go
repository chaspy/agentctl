package session

// GenerateAutoSummary creates a one-line summary from the last assistant message.
// Returns empty string if the last role is not "assistant" or the message is empty.
func GenerateAutoSummary(lastMessage, lastRole string) string {
	if lastRole != "assistant" || lastMessage == "" {
		return ""
	}
	msg := lastMessage
	if len(msg) > 80 {
		msg = msg[:80] + "..."
	}
	return msg
}
