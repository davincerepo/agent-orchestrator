package controllers

import (
	"fmt"
	"net/http"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

func validateMessageLength(w http.ResponseWriter, r *http.Request, message string) bool {
	if len(message) <= maxMessageLen {
		return true
	}
	envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "MESSAGE_TOO_LONG",
		fmt.Sprintf("Message is too long (%d bytes; maximum %d bytes / 1 MiB); write the content to a file accessible to the recipient and send its path", len(message), maxMessageLen), nil)
	return false
}
