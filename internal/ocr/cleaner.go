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

// Cleaner provides text cleaning functionality for OCR output.
type Cleaner struct {
	// Precompiled regex patterns for performance
	reHyphenJoin              *regexp.Regexp
	rePreprintToken           *regexp.Regexp
	reDetectedDiacriticsToken *regexp.Regexp
	reNoBestWordsToken        *regexp.Regexp
	rePunctOnlyLine           *regexp.Regexp
	reMultiSpace              *regexp.Regexp

	// Character replacer for common OCR artifacts
	charReplacer *strings.Replacer
}

// NewCleaner creates a new text cleaner with precompiled patterns.
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

// Clean applies comprehensive cleaning to OCR text output.
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

	scanner := bufio.NewScanner(strings.NewReader(input))

	// Increase scanner buffer to handle long lines
	const maxLineSize = 1024 * 1024

	buf := make([]byte, 0, initialBufferSize)
	scanner.Buffer(buf, maxLineSize)

	first := true

	for scanner.Scan() {
		line := scanner.Text()

		line = strings.TrimSpace(line)

		// Skip empty lines and punctuation-only lines
		if line == "" || c.rePunctOnlyLine.MatchString(line) {
			continue
		}

		// Collapse multiple spaces
		line = c.reMultiSpace.ReplaceAllString(line, " ")

		if !first {
			builder.WriteByte('\n')
		}

		first = false

		builder.WriteString(line)
	}

	// Check for scanner errors
	err := scanner.Err()
	if err != nil {
		// If scanner fails, return the original input
		return input
	}

	return builder.String()
}
