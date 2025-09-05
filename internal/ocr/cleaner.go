// Package ocr provides optical character recognition capabilities using Tesseract OCR.
// It includes comprehensive text cleaning and normalization to improve the quality
// of extracted text by removing common OCR artifacts and formatting inconsistencies.
//
// The package handles two main responsibilities:
// 1. Tesseract OCR engine integration with configurable parameters
// 2. Text cleaning and normalization for improved readability and consistency
//
// Text cleaning addresses common OCR issues including:
// - Hyphenated word reconstruction across line breaks
// - Removal of OCR-specific tokens and artifacts
// - Ligature replacement and character normalization
// - Whitespace and punctuation cleanup
//
// All cleaning operations are applied systematically to ensure consistent,
// high-quality text output suitable for further processing or direct use.
package ocr

import (
	"bufio"
	"regexp"
	"strings"
)

const (
	// Buffer sizes for scanner operations.
	initialBufferSize = 64 * 1024
)

// Cleaner provides comprehensive text cleaning functionality for OCR output.
// It addresses common OCR artifacts and formatting issues through a systematic
// application of regular expressions and character replacements.
//
// The cleaner handles multiple categories of OCR issues:
// - Line-level artifacts (hyphenation, punctuation-only lines)
// - Token-level artifacts (preprint notices, diacritic warnings)
// - Character-level issues (ligatures, special characters)
// - Spacing and formatting normalization
//
// All regular expressions are precompiled during initialization for optimal
// performance during repeated cleaning operations.
type Cleaner struct {
	// reHyphenJoin reconstructs words split across lines with hyphens
	// Example: "infor-\n mation" becomes "information"
	reHyphenJoin *regexp.Regexp

	// rePreprintToken removes "preprint" or "preprints" tokens with optional
	// apostrophes
	// Common in academic document OCR output
	rePreprintToken *regexp.Regexp

	// reDetectedDiacriticsToken removes Tesseract's diacritic detection warnings
	// Example: "detected 5 diacritics" is removed entirely
	reDetectedDiacriticsToken *regexp.Regexp

	// reNoBestWordsToken removes Tesseract's "no best words!" error messages
	// Appears when OCR confidence is extremely low
	reNoBestWordsToken *regexp.Regexp

	// rePunctOnlyLine identifies lines containing only punctuation and whitespace
	// These lines are typically OCR artifacts and should be removed
	rePunctOnlyLine *regexp.Regexp

	// reMultiSpace normalizes multiple consecutive spaces to single spaces
	// Improves text readability and consistency
	reMultiSpace *regexp.Regexp

	// charReplacer handles common OCR character substitutions
	// Primarily ligature replacements (ﬁ→fi, ﬂ→fl) and carriage return removal
	charReplacer *strings.Replacer
}

// NewCleaner creates a new text cleaner with all regular expressions precompiled
// for optimal performance. The cleaner is ready to use immediately after creation.
//
// All cleaning patterns are configured with appropriate flags for case-insensitive
// matching where relevant, and use Unicode-aware character classes for proper
// international text handling.
func NewCleaner() *Cleaner {
	return &Cleaner{
		// Join hyphenated line breaks like "infor-\n mation" -> "information"
		reHyphenJoin: regexp.MustCompile(`([a-z])-\s*\n\s*([a-z])`),

		// Remove tokens like Preprint / preprints with optional trailing
		// apostrophes
		rePreprintToken: regexp.MustCompile(`(?i)\bpreprints?\b'*`),

		// Remove diacritic detection notices
		reDetectedDiacriticsToken: regexp.MustCompile(
			`(?i)\bdetected\s+\d+\s+diacritics\b`,
		),

		// Remove noisy Tesseract phrases
		reNoBestWordsToken: regexp.MustCompile(`(?i)no\s+best\s+words!+`),

		// Lines that are only punctuation/space
		rePunctOnlyLine: regexp.MustCompile(`^\s*[\p{P}\s]+\s*$`),

		// Collapse multiple spaces
		reMultiSpace: regexp.MustCompile(`[ \t]{2,}`),

		// Character replacements for common OCR artifacts
		charReplacer: strings.NewReplacer(
			// Common ligatures
			"ﬁ", "fi",
			"ﬂ", "fl",
			"ﬀ", "ff",
			"ﬃ", "ffi",
			"ﬄ", "ffl",
			// Dashes and ellipsis
			"—", "--",
			"–", "--",
			"…", "...",
			// Carriage returns
			"\r", "",
		),
	}
}

// Clean performs comprehensive text cleaning and normalization on OCR output.
// It systematically addresses common OCR artifacts and formatting issues through
// a multi-stage process designed to maximize text quality and readability.
//
// The cleaning process applies these transformations in order:
//
//  1. Character-level replacements: Converts ligatures (ﬁ→fi, ﬂ→fl) and removes
//     carriage returns that interfere with text processing
//
//  2. OCR artifact removal: Eliminates Tesseract-specific tokens like "preprint",
//     "detected N diacritics", and "no best words!" that appear in low-quality OCR
//
//  3. Hyphenation reconstruction: Rejoins words split across line breaks with
//     hyphens (e.g., "infor-\n mation" becomes "information")
//
//  4. Line-level cleaning: Removes punctuation-only lines and normalizes whitespace
//     while preserving meaningful line breaks and paragraph structure
//
// The function returns cleaned text with leading/trailing whitespace trimmed.
// Empty input returns empty output without processing overhead.
//
// Example transformations:
//
//	Input:  "Hello    World\n!!!\nThis is ﬁne"
//	Output: "Hello World\nThis is fine"
func (c *Cleaner) Clean(input string) string {
	if input == "" {
		return input
	}

	// 1. Apply character-level replacements
	text := c.charReplacer.Replace(input)

	// 2. Remove obvious Tesseract artifacts (case-insensitive)
	text = c.rePreprintToken.ReplaceAllString(text, "")
	text = c.reDetectedDiacriticsToken.ReplaceAllString(text, "")
	text = c.reNoBestWordsToken.ReplaceAllString(text, "")

	// 3. Fix hyphenated line breaks across lines
	text = c.reHyphenJoin.ReplaceAllString(text, "$1$2")

	// 4. Normalize whitespace line-by-line and drop punctuation-only lines
	text = c.cleanLines(text)

	return strings.TrimSpace(text)
}

// cleanLines processes text line by line to remove empty and punctuation-only lines.
func (c *Cleaner) cleanLines(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))

	scanner := c.createScanner(input)
	c.processScannedLines(scanner, &builder)

	err := scanner.Err()
	if err != nil {
		return input
	}

	return builder.String()
}

func (c *Cleaner) processScannedLines(scanner *bufio.Scanner, builder *strings.Builder) {
	first := true

	for scanner.Scan() {
		line := c.processLine(scanner.Text())
		if line == "" {
			continue
		}

		c.addLineToBuilder(builder, line, &first)
	}
}

func (c *Cleaner) addLineToBuilder(builder *strings.Builder, line string, first *bool) {
	if !*first {
		builder.WriteByte('\n')
	}

	*first = false

	builder.WriteString(line)
}

func (c *Cleaner) createScanner(input string) *bufio.Scanner {
	scanner := bufio.NewScanner(strings.NewReader(input))

	const maxLineSize = 1024 * 1024

	buf := make([]byte, 0, initialBufferSize)
	scanner.Buffer(buf, maxLineSize)

	return scanner
}

func (c *Cleaner) processLine(line string) string {
	line = strings.TrimSpace(line)

	if line == "" || c.rePunctOnlyLine.MatchString(line) {
		return ""
	}

	return c.reMultiSpace.ReplaceAllString(line, " ")
}
