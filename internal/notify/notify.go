// Package notify wraps beeep for cross-platform desktop notifications.
package notify

import "github.com/gen2brain/beeep"

// Send sends a desktop notification with a title and message.
func Send(title, message string) error {
	return beeep.Notify(title, message, "")
}
