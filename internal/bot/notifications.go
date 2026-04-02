package bot

import "fmt"

// BuildNotificationMessage formats a notification for sending via the bot.
func BuildNotificationMessage(title, text string) string {
	if title == "" {
		return text
	}
	return fmt.Sprintf("🔔 **%s**\n\n%s", title, text)
}
