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
type Cleaner struct {
	reHyphenJoin              *regexp.Regexp
	rePreprintToken           *regexp.Regexp
	reDetectedDiacriticsToken *regexp.Regexp
	reNoBestWordsToken        *regexp.Regexp
	rePunctOnlyLine           *regexp.Regexp
	reMultiSpace              *regexp.Regexp
	rePageNumber              *regexp.Regexp
	charReplacer              *strings.Replacer
}

// NewCleaner creates a new text cleaner with all regular expressions precompiled.
func NewCleaner() *Cleaner {
	return &Cleaner{
		reHyphenJoin:    regexp.MustCompile(`([a-z])-\s*\n\s*([a-z])`),
		rePreprintToken: regexp.MustCompile(`(?i)\bpreprints?\b'*`),
		reDetectedDiacriticsToken: regexp.MustCompile(
			`(?i)\bdetected\s+\d+\s+diacritics\b`,
		),
		reNoBestWordsToken: regexp.MustCompile(`(?i)no\s+best\s+words!+`),
		rePunctOnlyLine:    regexp.MustCompile(`^\s*[\p{P}\s]+\s*$`),
		reMultiSpace:       regexp.MustCompile(`[ \t]{2,}`),
		rePageNumber:       regexp.MustCompile(`(?m)^\s*\d+\s*$`),
		charReplacer: strings.NewReplacer(
			"ﬁ", "fi",
			"ﬂ", "fl",
			"ﬀ", "ff",
			"ﬃ", "ffi",
			"ﬄ", "ffl",
			"—", "--",
			"–", "--",
			"…", "...",
			"\r", "",
		),
	}
}

// Clean performs comprehensive text cleaning and normalization on OCR output.
func (c *Cleaner) Clean(input string) string {
	if input == "" {
		return input
	}

	// 1. Apply character-level replacements
	text := c.charReplacer.Replace(input)

	// 2. Remove obvious Tesseract artifacts
	text = c.rePreprintToken.ReplaceAllString(text, "")
	text = c.reDetectedDiacriticsToken.ReplaceAllString(text, "")
	text = c.reNoBestWordsToken.ReplaceAllString(text, "")
	text = c.rePageNumber.ReplaceAllString(text, "")

	// 3. Fix hyphenated line breaks
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
