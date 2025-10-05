// Package shared provides types that are shared across multiple packages in the
// png-to-text-service to avoid circular dependencies.
package shared

import "github.com/book-expert/events"

// SummaryPlacement identifies where page-level summaries should be inserted relative to OCR text.
type SummaryPlacement = events.SummaryPlacement

// AugmentationCommentaryOptions captures commentary specific behaviour.
type AugmentationCommentaryOptions struct {
	Enabled         bool   `json:"enabled"`
	CustomAdditions string `json:"customAdditions"`
}

// AugmentationSummaryOptions captures summary specific behaviour.
type AugmentationSummaryOptions struct {
	Enabled         bool             `json:"enabled"`
	Placement       SummaryPlacement `json:"placement"`
	CustomAdditions string           `json:"customAdditions"`
}

// AugmentationOptions holds options for text augmentation.
type AugmentationOptions struct {
	Parameters map[string]any                `json:"parameters"`
	Commentary AugmentationCommentaryOptions `json:"commentary"`
	Summary    AugmentationSummaryOptions    `json:"summary"`
}
