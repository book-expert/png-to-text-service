package events

import "time"

// EventHeader contains metadata common to all events.
type EventHeader struct {
	Timestamp  time.Time `json:"timestamp"`
	WorkflowID string    `json:"workflow_id"`
	UserID     string    `json:"user_id"`
	TenantID   string    `json:"tenant_id"`
	EventID    string    `json:"event_id"`
}

// PNGCreatedEvent is triggered when a PNG page is generated from a PDF.
type PNGCreatedEvent struct {
	Header     EventHeader `json:"header"`
	PNGKey     string      `json:"png_key"`
	PageNumber int         `json:"page_number"`
	TotalPages int         `json:"total_pages"`
}

// TextProcessedEvent is triggered after text has been extracted from a PNG.
type TextProcessedEvent struct {
	Header     EventHeader `json:"header"`
	PNGKey     string      `json:"png_key"`
	TextKey    string      `json:"text_key"`
	PageNumber int         `json:"page_number"`
	TotalPages int         `json:"total_pages"`
}
