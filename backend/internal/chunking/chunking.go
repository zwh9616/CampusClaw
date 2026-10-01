// Package chunking turns a stored teaching text into ordered, traceable
// slices.
//
// Every slice carries the Unicode character range it was taken from, and its
// text is exactly that range of the text that was actually chunked. The range
// is therefore checkable by slicing: a caller can always re-derive a chunk's
// text from the stored body and the recorded configuration, which is what makes
// a citation verifiable rather than merely plausible.
package chunking

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidOptions marks a split request the API must answer with 400. Every
// rejection is wrapped in it so the HTTP layer never has to read error text.
var ErrInvalidOptions = errors.New("invalid chunk options")

// Strategy names one of the three supported splitting behaviours.
type Strategy string

const (
	// StrategyAuto is the default: 800 characters, 80 characters of overlap,
	// preferring a blank line, then a newline, then a full stop.
	StrategyAuto Strategy = "auto"
	// StrategyCustom takes an explicit separator, length and overlap, and may
	// preprocess the text before splitting.
	StrategyCustom Strategy = "custom"
	// StrategyHierarchy keeps Markdown sections together and falls back to the
	// auto rules for a section longer than the maximum.
	StrategyHierarchy Strategy = "hierarchy"
)

// Separator is a custom split's preferred break.
type Separator string

const (
	SeparatorNewline   Separator = "newline"
	SeparatorBlankLine Separator = "blank_line"
	SeparatorSentence  Separator = "sentence"
)

// OffsetBasis records which text a character range indexes into. It exists so a
// range is never mistaken for a page number or a byte offset in the original
// file: 'normalized' means the text was preprocessed before splitting.
type OffsetBasis string

const (
	// BasisExtracted: the range indexes the stored body text unchanged.
	BasisExtracted OffsetBasis = "extracted"
	// BasisNormalized: the range indexes the preprocessed text.
	BasisNormalized OffsetBasis = "normalized"
)

// The bounds KR-01 fixes.
const (
	// AutoMaxChars and AutoOverlapPercent are the default window, which is also
	// what every backfill uses.
	AutoMaxChars       = 800
	AutoOverlapPercent = 10

	MinCustomMaxChars = 100
	MaxCustomMaxChars = 2000
	MinOverlapPercent = 0
	MaxOverlapPercent = 50
)

// Options is a validated split request.
//
// MaxChars and OverlapPercent are always populated: a strategy that does not
// take them is fixed to the auto values rather than left at zero, so the
// caller never has to special-case them.
type Options struct {
	Strategy         Strategy
	MaxChars         int
	OverlapPercent   int
	Separator        Separator
	RemoveURLsEmails bool
	FoldWhitespace   bool
}

// Chunk is one slice of the chunked text.
//
// Start is inclusive and End is exclusive, both counted in Unicode characters
// of the chunked text. Text is always exactly that range, so
// []rune(source)[Start:End] == Text holds.
type Chunk struct {
	Index int
	Text  string
	Start int
	End   int
}

// Result is a complete split.
type Result struct {
	Chunks []Chunk
	// Basis says which text Chunks index into.
	Basis OffsetBasis
	// Options is the configuration that produced the split, recorded so an
	// index generation can be explained and reproduced.
	Options Options
}

// Request is the untrusted split request as it arrives from a multipart form.
// Empty fields mean "not supplied", which is what lets the auto strategy ignore
// parameters a client should not have sent.
type Request struct {
	Strategy         string
	MaxChars         string
	OverlapPercent   string
	Separator        string
	RemoveURLsEmails string
	FoldWhitespace   string
}

// ParseRequest validates a raw request and fills in the values a strategy does
// not take.
//
// An unknown strategy, an out-of-range custom parameter, or an unparseable
// flag is rejected: accepting it would silently index the material under a
// configuration nobody asked for.
func ParseRequest(request Request) (Options, error) {
	strategy := Strategy(strings.TrimSpace(request.Strategy))
	if strategy == "" {
		strategy = StrategyAuto
	}

	switch strategy {
	case StrategyAuto, StrategyHierarchy:
		// The extra fields are ignored on purpose: auto and hierarchy have
		// fixed parameters, and a client that sends others is not an error.
		//
		// The separator is recorded as the default even though these strategies
		// do not consult it, because the stored row requires one of the known
		// values and an empty string would read as an unset parameter.
		return Options{
			Strategy:       strategy,
			MaxChars:       AutoMaxChars,
			OverlapPercent: AutoOverlapPercent,
			Separator:      SeparatorNewline,
		}, nil

	case StrategyCustom:
		return parseCustom(request)

	default:
		return Options{}, fmt.Errorf("%w: unknown strategy %q", ErrInvalidOptions, request.Strategy)
	}
}

func parseCustom(request Request) (Options, error) {
	options := Options{
		Strategy:       StrategyCustom,
		MaxChars:       AutoMaxChars,
		OverlapPercent: AutoOverlapPercent,
		Separator:      SeparatorNewline,
	}

	if raw := strings.TrimSpace(request.MaxChars); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < MinCustomMaxChars || value > MaxCustomMaxChars {
			return Options{}, fmt.Errorf(
				"%w: chunk_max_chars must be between %d and %d",
				ErrInvalidOptions, MinCustomMaxChars, MaxCustomMaxChars,
			)
		}
		options.MaxChars = value
	}

	if raw := strings.TrimSpace(request.OverlapPercent); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < MinOverlapPercent || value > MaxOverlapPercent {
			return Options{}, fmt.Errorf(
				"%w: chunk_overlap_percent must be between %d and %d",
				ErrInvalidOptions, MinOverlapPercent, MaxOverlapPercent,
			)
		}
		options.OverlapPercent = value
	}

	if raw := strings.TrimSpace(request.Separator); raw != "" {
		switch Separator(raw) {
		case SeparatorNewline, SeparatorBlankLine, SeparatorSentence:
			options.Separator = Separator(raw)
		default:
			return Options{}, fmt.Errorf("%w: unknown chunk_separator %q", ErrInvalidOptions, raw)
		}
	}

	removeURLs, err := parseFlag("remove_urls_emails", request.RemoveURLsEmails)
	if err != nil {
		return Options{}, err
	}
	foldWhitespace, err := parseFlag("fold_whitespace", request.FoldWhitespace)
	if err != nil {
		return Options{}, err
	}

	options.RemoveURLsEmails = removeURLs
	options.FoldWhitespace = foldWhitespace

	return options, nil
}

// parseFlag accepts the values a form control can send for a checkbox.
func parseFlag(name, raw string) (bool, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "false", "0", "off":
		return false, nil
	case "true", "1", "on":
		return true, nil
	default:
		return false, fmt.Errorf("%w: %s must be a boolean", ErrInvalidOptions, name)
	}
}

// Validate re-checks an Options value that was built in code rather than parsed
// from a request, so an internal caller cannot construct a window that would
// never terminate.
func (o Options) Validate() error {
	switch o.Strategy {
	case StrategyAuto, StrategyHierarchy:
		if o.MaxChars != AutoMaxChars || o.OverlapPercent != AutoOverlapPercent {
			return fmt.Errorf("%w: %s uses the fixed %d/%d window",
				ErrInvalidOptions, o.Strategy, AutoMaxChars, AutoOverlapPercent)
		}
	case StrategyCustom:
		if o.MaxChars < MinCustomMaxChars || o.MaxChars > MaxCustomMaxChars {
			return fmt.Errorf("%w: chunk_max_chars must be between %d and %d",
				ErrInvalidOptions, MinCustomMaxChars, MaxCustomMaxChars)
		}
		if o.OverlapPercent < MinOverlapPercent || o.OverlapPercent > MaxOverlapPercent {
			return fmt.Errorf("%w: chunk_overlap_percent must be between %d and %d",
				ErrInvalidOptions, MinOverlapPercent, MaxOverlapPercent)
		}
		switch o.Separator {
		case SeparatorNewline, SeparatorBlankLine, SeparatorSentence:
		default:
			return fmt.Errorf("%w: unknown chunk_separator %q", ErrInvalidOptions, o.Separator)
		}
	default:
		return fmt.Errorf("%w: unknown strategy %q", ErrInvalidOptions, o.Strategy)
	}

	return nil
}

// Preprocessed reports whether this configuration rewrites the text before
// splitting, which is what decides the offset basis.
func (o Options) Preprocessed() bool {
	return o.Strategy == StrategyCustom && (o.RemoveURLsEmails || o.FoldWhitespace)
}

// overlapChars is the window overlap in characters. It is always strictly
// smaller than the window, so every step of the slide makes progress.
func (o Options) overlapChars() int {
	return o.MaxChars * o.OverlapPercent / 100
}

// markers lists the preferred break points in priority order. A custom split
// has exactly one; the auto rules try the blank line, then the newline, then
// the full stop.
func (o Options) markers() [][]rune {
	if o.Strategy == StrategyCustom {
		switch o.Separator {
		case SeparatorBlankLine:
			return [][]rune{{'\n', '\n'}}
		case SeparatorSentence:
			return [][]rune{{'。'}}
		default:
			return [][]rune{{'\n'}}
		}
	}

	return [][]rune{{'\n', '\n'}, {'\n'}, {'。'}}
}

// Split slices body into ordered chunks.
//
// The body itself is never modified: preprocessing, when configured, produces a
// separate source string that the returned ranges index into.
func Split(body string, options Options) (Result, error) {
	if err := options.Validate(); err != nil {
		return Result{}, err
	}

	source := []rune(body)
	basis := BasisExtracted

	if options.Preprocessed() {
		source = []rune(Normalize(body, options))
		basis = BasisNormalized
	}

	var chunks []Chunk
	if options.Strategy == StrategyHierarchy {
		chunks = splitHierarchy(source, options)
	} else {
		chunks = splitWindows(source, options)
	}

	return Result{Chunks: chunks, Basis: basis, Options: options}, nil
}

// ChunkSource returns the exact text the result's ranges index into, so a
// stored range can be re-checked without re-running the split.
func ChunkSource(body string, options Options) string {
	if options.Preprocessed() {
		return Normalize(body, options)
	}
	return body
}
