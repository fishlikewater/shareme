package app

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"message-share/backend/internal/domain"
)

const messageHistoryPageSize = 10

type MessageHistoryPageSnapshot struct {
	ConversationID string            `json:"conversationId"`
	Messages       []MessageSnapshot `json:"messages"`
	HasMore        bool              `json:"hasMore"`
	NextCursor     string            `json:"nextCursor,omitempty"`
}

func encodeMessageCursor(boundary domain.MessageBoundary) string {
	if boundary.CreatedAt.IsZero() || strings.TrimSpace(boundary.MessageID) == "" {
		return ""
	}

	raw := boundary.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + boundary.MessageID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeMessageCursor(value string) (domain.MessageBoundary, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return domain.MessageBoundary{}, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return domain.MessageBoundary{}, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return domain.MessageBoundary{}, fmt.Errorf("invalid cursor")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return domain.MessageBoundary{}, fmt.Errorf("parse cursor time: %w", err)
	}

	return domain.MessageBoundary{
		CreatedAt: createdAt.UTC(),
		MessageID: parts[1],
	}, nil
}
