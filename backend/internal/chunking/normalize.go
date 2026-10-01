package chunking

import (
	"regexp"
	"strings"
	"unicode"
)

// The preprocessing patterns. A URL is anything with an http(s) or www prefix
// up to the next whitespace; an email is the usual local@domain.tld shape.
var (
	urlPattern   = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// Normalize applies the preprocessing a custom split may ask for.
//
// It returns a new string and never touches the stored body: the ranges the
// split reports index this text, and the caller records that fact as the offset
// basis so the two are never confused.
func Normalize(body string, options Options) string {
	text := body

	if options.RemoveURLsEmails {
		// Removal runs before folding so the whitespace a removed URL leaves
		// behind is collapsed along with everything else.
		text = urlPattern.ReplaceAllString(text, "")
		text = emailPattern.ReplaceAllString(text, "")
	}

	if options.FoldWhitespace {
		text = foldWhitespace(text)
	}

	return text
}

// foldWhitespace replaces every run of whitespace with a single space. Runs at
// the very start or end of the text collapse too, so the mapping stays exactly
// "run of whitespace becomes one space".
func foldWhitespace(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))

	inWhitespace := false

	for _, r := range text {
		if unicode.IsSpace(r) {
			if inWhitespace {
				continue
			}
			inWhitespace = true
			builder.WriteRune(' ')
			continue
		}

		inWhitespace = false
		builder.WriteRune(r)
	}

	return builder.String()
}
