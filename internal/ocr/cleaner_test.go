package ocr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnikolov3/png-to-text-service/internal/ocr"
)

func TestNewCleaner(t *testing.T) {
	t.Parallel()

	cleaner := ocr.NewCleaner()

	require.NotNil(t, cleaner)
}

func TestClean_BasicFunctionality(t *testing.T) {
	t.Parallel()

	cleaner := ocr.NewCleaner()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "simple text unchanged",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "whitespace normalization",
			input:    "Hello    World",
			expected: "Hello World",
		},
		{
			name:     "removes preprint tokens",
			input:    "Hello preprints World",
			expected: "Hello World",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := cleaner.Clean(testCase.input)
			require.Equal(t, testCase.expected, result)
		})
	}
}

func TestClean_AdvancedFunctionality(t *testing.T) {
	t.Parallel()

	cleaner := ocr.NewCleaner()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "hyphen joining across lines",
			input:    "infor-\n mation",
			expected: "information",
		},
		{
			name:     "removes detected diacritics token",
			input:    "Hello detected 5 diacritics World",
			expected: "Hello World",
		},
		{
			name:     "removes no best words token",
			input:    "Hello no best words! World",
			expected: "Hello World",
		},
		{
			name:     "ligature replacement",
			input:    "ﬁle ﬂow",
			expected: "file flow",
		},
		{
			name:     "removes punctuation-only lines",
			input:    "Hello\n!!!\nWorld",
			expected: "Hello\nWorld",
		},
		{
			name:     "removes carriage returns",
			input:    "Hello\rWorld",
			expected: "HelloWorld",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := cleaner.Clean(testCase.input)
			require.Equal(t, testCase.expected, result)
		})
	}
}
