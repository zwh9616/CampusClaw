package chunking_test

import (
	"errors"
	"strings"
	"testing"

	"campusclaw/internal/chunking"
)

// autoOptions is the default split a request with no strategy produces.
func autoOptions(t *testing.T) chunking.Options {
	t.Helper()

	options, err := chunking.ParseRequest(chunking.Request{})
	if err != nil {
		t.Fatalf("parse empty request: %v", err)
	}
	return options
}

// check verifies the invariants every split must satisfy: a chunk's text is
// exactly its recorded range, ranges are ordered and non-empty, and indices are
// the 0..n-1 sequence.
func check(t *testing.T, source string, result chunking.Result) {
	t.Helper()

	runes := []rune(source)

	for position, chunk := range result.Chunks {
		if chunk.Index != position {
			t.Errorf("chunk %d has index %d, want %d", position, chunk.Index, position)
		}
		if chunk.Start >= chunk.End {
			t.Fatalf("chunk %d has an empty range [%d,%d)", position, chunk.Start, chunk.End)
		}
		if chunk.Start < 0 || chunk.End > len(runes) {
			t.Fatalf("chunk %d range [%d,%d) is outside the text of %d characters",
				position, chunk.Start, chunk.End, len(runes))
		}
		if got := string(runes[chunk.Start:chunk.End]); got != chunk.Text {
			t.Errorf("chunk %d text does not match its range:\n got %q\nwant %q",
				position, chunk.Text, got)
		}
		if strings.TrimSpace(chunk.Text) != chunk.Text {
			t.Errorf("chunk %d is padded with whitespace: %q", position, chunk.Text)
		}
		if position > 0 && chunk.Start < result.Chunks[position-1].Start {
			t.Errorf("chunk %d starts before its predecessor", position)
		}
	}
}

// The default split is a fixed window: a text with no break at all is cut every
// 800 characters with 80 characters of overlap.
func TestAutoSplitsALongRunIntoOverlappingWindows(t *testing.T) {
	body := strings.Repeat("字", 2000)
	options := autoOptions(t)

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(result.Chunks))
	}

	want := []chunking.Chunk{
		{Index: 0, Start: 0, End: 800},
		{Index: 1, Start: 720, End: 1520},
		{Index: 2, Start: 1440, End: 2000},
	}

	for index, expected := range want {
		got := result.Chunks[index]
		if got.Start != expected.Start || got.End != expected.End {
			t.Errorf("chunk %d range = [%d,%d), want [%d,%d)",
				index, got.Start, got.End, expected.Start, expected.End)
		}
	}

	if result.Basis != chunking.BasisExtracted {
		t.Errorf("Basis = %q, want %q", result.Basis, chunking.BasisExtracted)
	}
}

// A blank line is the most preferred break, so a window that contains one ends
// there rather than at the hard limit.
func TestAutoPrefersABlankLineOverTheHardLimit(t *testing.T) {
	body := strings.Repeat("甲", 698) + "\n\n" + strings.Repeat("乙", 160)
	options := autoOptions(t)

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) == 0 {
		t.Fatal("got no chunks")
	}

	first := result.Chunks[0]
	if first.End != 698 {
		t.Errorf("first chunk ends at %d, want the blank line at 698", first.End)
	}
	if strings.ContainsAny(first.Text, "\n ") {
		t.Errorf("first chunk kept the break whitespace: %q", first.Text)
	}
}

// With no line break available the full stop is used instead.
func TestAutoFallsBackToTheFullStop(t *testing.T) {
	body := strings.Repeat("甲", 700) + "。" + strings.Repeat("乙", 200)
	options := autoOptions(t)

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) == 0 {
		t.Fatal("got no chunks")
	}

	if !strings.HasSuffix(result.Chunks[0].Text, "。") {
		t.Errorf("first chunk does not end at the full stop: %q", tail(result.Chunks[0].Text, 12))
	}
	if result.Chunks[0].End != 701 {
		t.Errorf("first chunk ends at %d, want 701", result.Chunks[0].End)
	}
}

// Custom takes the separator, the window and the overlap explicitly.
func TestCustomHonoursLengthAndOverlap(t *testing.T) {
	body := strings.Repeat("a", 250)

	options, err := chunking.ParseRequest(chunking.Request{
		Strategy:       "custom",
		MaxChars:       "100",
		OverlapPercent: "50",
		Separator:      "newline",
	})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	want := [][2]int{{0, 100}, {50, 150}, {100, 200}, {150, 250}}
	if len(result.Chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d", len(result.Chunks), len(want))
	}

	for index, expected := range want {
		chunk := result.Chunks[index]
		if chunk.Start != expected[0] || chunk.End != expected[1] {
			t.Errorf("chunk %d range = [%d,%d), want [%d,%d)",
				index, chunk.Start, chunk.End, expected[0], expected[1])
		}
	}
}

// A window with no chosen separator inside it is cut by length.
func TestCustomTruncatesWhenTheSeparatorIsAbsent(t *testing.T) {
	body := strings.Repeat("字", 300)

	options, err := chunking.ParseRequest(chunking.Request{
		Strategy:       "custom",
		MaxChars:       "200",
		OverlapPercent: "0",
		Separator:      "sentence",
	})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(result.Chunks))
	}
	if result.Chunks[0].End != 200 {
		t.Errorf("first chunk ends at %d, want the hard limit 200", result.Chunks[0].End)
	}
}

// The blank-line separator only breaks at a blank line, even when a single
// newline is nearer.
func TestCustomBlankLineSeparatorIgnoresSingleNewlines(t *testing.T) {
	body := strings.Repeat("甲", 120) + "\n" + strings.Repeat("乙", 120)

	options, err := chunking.ParseRequest(chunking.Request{
		Strategy:       "custom",
		MaxChars:       "200",
		OverlapPercent: "0",
		Separator:      "blank_line",
	})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	// The single newline is not a break for this separator, so the first
	// window is cut at the limit and keeps the newline inside it.
	if result.Chunks[0].End != 200 {
		t.Errorf("first chunk ends at %d, want the hard limit 200", result.Chunks[0].End)
	}
}

// Preprocessing changes only what is indexed: the body is untouched, the ranges
// index the preprocessed text, and the basis says so.
func TestCustomPreprocessingKeepsTheBodyAndRetargetsTheRanges(t *testing.T) {
	body := "访问 https://example.com/a 或 mailto 联系 teacher@example.com\n\n\n下一段"
	unchanged := body

	options, err := chunking.ParseRequest(chunking.Request{
		Strategy:         "custom",
		MaxChars:         "100",
		OverlapPercent:   "0",
		Separator:        "blank_line",
		RemoveURLsEmails: "on",
		FoldWhitespace:   "true",
	})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	if body != unchanged {
		t.Errorf("the body was modified:\n got %q\nwant %q", body, unchanged)
	}

	if result.Basis != chunking.BasisNormalized {
		t.Errorf("Basis = %q, want %q", result.Basis, chunking.BasisNormalized)
	}

	source := chunking.ChunkSource(body, options)
	const wantSource = "访问 或 mailto 联系 下一段"
	if source != wantSource {
		t.Errorf("chunk source = %q, want %q", source, wantSource)
	}

	check(t, source, result)

	for _, chunk := range result.Chunks {
		if strings.Contains(chunk.Text, "example.com") {
			t.Errorf("a removed URL or address survives in chunk %d: %q", chunk.Index, chunk.Text)
		}
		if strings.Contains(chunk.Text, "\n\n\n") {
			t.Errorf("whitespace was not folded in chunk %d: %q", chunk.Index, chunk.Text)
		}
	}
}

// Without preprocessing the ranges index the body itself, so the source helper
// is the identity.
func TestChunkSourceIsTheBodyWithoutPreprocessing(t *testing.T) {
	body := "短文本"
	options := autoOptions(t)

	if got := chunking.ChunkSource(body, options); got != body {
		t.Errorf("ChunkSource() = %q, want the body unchanged", got)
	}
}

// Hierarchy keeps each chapter whole, keeps its heading inside the chunk, and
// only falls back to the window rules for an over-long chapter.
func TestHierarchyKeepsHeadingsWithTheirChapter(t *testing.T) {
	body := "# 第一章\n" + strings.Repeat("甲", 20) + "\n" +
		"## 第一节\n" + strings.Repeat("乙", 900) + "\n" +
		"#### 不是标题\n" + strings.Repeat("丙", 5)

	options, err := chunking.ParseRequest(chunking.Request{Strategy: "hierarchy"})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) < 3 {
		t.Fatalf("got %d chunks, want the short chapter, the split long one and the tail", len(result.Chunks))
	}

	if !strings.HasPrefix(result.Chunks[0].Text, "# 第一章") {
		t.Errorf("first chunk does not open with its heading: %q", tail(result.Chunks[0].Text, 20))
	}

	// The second chapter is over the window, so it is split, and its first
	// piece still carries the heading.
	if !strings.HasPrefix(result.Chunks[1].Text, "## 第一节") {
		t.Errorf("the over-long chapter lost its heading: %q", tail(result.Chunks[1].Text, 20))
	}

	// Every chunk stays within the auto window.
	for _, chunk := range result.Chunks {
		if length := len([]rune(chunk.Text)); length > chunking.AutoMaxChars {
			t.Errorf("chunk %d is %d characters, over the %d limit",
				chunk.Index, length, chunking.AutoMaxChars)
		}
	}

	// A four-hash line is not a heading, so it stays inline instead of
	// starting a chunk of its own.
	joined := joinTexts(result)
	if !strings.Contains(joined, "#### 不是标题") {
		t.Error("the content after the four-hash line was dropped")
	}
}

// Text before the first heading is indexed too, rather than being discarded.
func TestHierarchyKeepsThePreamble(t *testing.T) {
	body := "前言\n# 标题\n正文"

	options, err := chunking.ParseRequest(chunking.Request{Strategy: "hierarchy"})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) == 0 || !strings.HasPrefix(result.Chunks[0].Text, "前言") {
		t.Fatalf("the preamble is missing: %+v", result.Chunks)
	}
}

// A heading followed by one very long line must not break right after the
// heading: a handful of characters is not a useful chunk.
func TestHierarchyDoesNotEmitATinyChunkAfterAHeading(t *testing.T) {
	body := "# 标题\n" + strings.Repeat("长", 1500)

	options, err := chunking.ParseRequest(chunking.Request{Strategy: "hierarchy"})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	for _, chunk := range result.Chunks {
		if length := len([]rune(chunk.Text)); length < chunking.AutoMaxChars/2 {
			t.Errorf("chunk %d is only %d characters: %q", chunk.Index, length, tail(chunk.Text, 20))
		}
	}
}

func TestHierarchyWithNoHeadingIsOneChunk(t *testing.T) {
	body := "没有标题的正文"

	options, err := chunking.ParseRequest(chunking.Request{Strategy: "hierarchy"})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) != 1 || result.Chunks[0].Text != body {
		t.Fatalf("got %+v, want the whole text as one chunk", result.Chunks)
	}
}

// An empty body is not an error; it simply has nothing to index.
func TestSplitOfEmptyTextYieldsNoChunks(t *testing.T) {
	for _, body := range []string{"", "   \n\n  "} {
		result, err := chunking.Split(body, autoOptions(t))
		if err != nil {
			t.Fatalf("Split(%q) error = %v", body, err)
		}
		if len(result.Chunks) != 0 {
			t.Errorf("Split(%q) = %+v, want no chunks", body, result.Chunks)
		}
	}
}

// Ranges are counted in Unicode characters, so a chunk never splits a
// multi-byte character or an emoji.
func TestRangesAreCountedInUnicodeCharacters(t *testing.T) {
	body := strings.Repeat("中文🙂", 400) // 1200 characters, 3600+ bytes

	result, err := chunking.Split(body, autoOptions(t))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) < 2 {
		t.Fatalf("got %d chunks, want the text split", len(result.Chunks))
	}
}

func TestParseRequestDefaultsToAuto(t *testing.T) {
	options, err := chunking.ParseRequest(chunking.Request{})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if options.Strategy != chunking.StrategyAuto {
		t.Errorf("Strategy = %q, want auto", options.Strategy)
	}
	if options.MaxChars != chunking.AutoMaxChars || options.OverlapPercent != chunking.AutoOverlapPercent {
		t.Errorf("window = %d/%d%%, want %d/%d%%",
			options.MaxChars, options.OverlapPercent, chunking.AutoMaxChars, chunking.AutoOverlapPercent)
	}
}

// Auto and hierarchy have fixed parameters, so extra fields are ignored rather
// than rejected and rather than taking effect.
func TestParseRequestIgnoresExtraFieldsForFixedStrategies(t *testing.T) {
	for _, strategy := range []string{"auto", "hierarchy"} {
		options, err := chunking.ParseRequest(chunking.Request{
			Strategy:         strategy,
			MaxChars:         "1500",
			OverlapPercent:   "40",
			Separator:        "sentence",
			RemoveURLsEmails: "on",
			FoldWhitespace:   "on",
		})
		if err != nil {
			t.Fatalf("%s: ParseRequest() error = %v", strategy, err)
		}

		if options.MaxChars != chunking.AutoMaxChars || options.OverlapPercent != chunking.AutoOverlapPercent {
			t.Errorf("%s: window = %d/%d%%, want the fixed auto window",
				strategy, options.MaxChars, options.OverlapPercent)
		}
		if options.RemoveURLsEmails || options.FoldWhitespace {
			t.Errorf("%s: preprocessing was enabled for a strategy that does not take it", strategy)
		}
		if options.Preprocessed() {
			t.Errorf("%s: Preprocessed() = true, want false", strategy)
		}
	}
}

func TestParseRequestRejectsInvalidInput(t *testing.T) {
	cases := map[string]chunking.Request{
		"unknown strategy":       {Strategy: "semantic"},
		"max chars below range":  {Strategy: "custom", MaxChars: "99"},
		"max chars above range":  {Strategy: "custom", MaxChars: "2001"},
		"max chars not a number": {Strategy: "custom", MaxChars: "wide"},
		"overlap above range":    {Strategy: "custom", OverlapPercent: "51"},
		"overlap below range":    {Strategy: "custom", OverlapPercent: "-1"},
		"unknown separator":      {Strategy: "custom", Separator: "comma"},
		"unparseable urls flag":  {Strategy: "custom", RemoveURLsEmails: "maybe"},
		"unparseable whitespace": {Strategy: "custom", FoldWhitespace: "sometimes"},
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := chunking.ParseRequest(request); err == nil {
				t.Fatalf("ParseRequest(%+v) succeeded, want a rejection", request)
			} else if !errors.Is(err, chunking.ErrInvalidOptions) {
				t.Errorf("error %v does not wrap ErrInvalidOptions", err)
			}
		})
	}
}

func TestParseRequestAcceptsTheCustomBounds(t *testing.T) {
	for _, request := range []chunking.Request{
		{Strategy: "custom", MaxChars: "100", OverlapPercent: "0"},
		{Strategy: "custom", MaxChars: "2000", OverlapPercent: "50"},
		{Strategy: "custom", Separator: "sentence"},
		{Strategy: "custom", Separator: "blank_line", FoldWhitespace: "off"},
	} {
		if _, err := chunking.ParseRequest(request); err != nil {
			t.Errorf("ParseRequest(%+v) error = %v", request, err)
		}
	}
}

// A request that names only the strategy must produce a usable window, so a
// custom split without explicit numbers is not an empty chunk list.
func TestCustomWithoutExplicitNumbersUsesTheDefaults(t *testing.T) {
	options, err := chunking.ParseRequest(chunking.Request{Strategy: "custom"})
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if options.MaxChars != chunking.AutoMaxChars || options.OverlapPercent != chunking.AutoOverlapPercent {
		t.Errorf("window = %d/%d%%, want the defaults %d/%d%%",
			options.MaxChars, options.OverlapPercent, chunking.AutoMaxChars, chunking.AutoOverlapPercent)
	}

	body := strings.Repeat("字", 1700)
	result, err := chunking.Split(body, options)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	check(t, body, result)

	if len(result.Chunks) < 2 {
		t.Errorf("got %d chunks, want the text split by the default window", len(result.Chunks))
	}
}

// Validate guards the constructor path, where Options is built in code rather
// than parsed, so an impossible window cannot reach the splitter.
func TestValidateRejectsAnImpossibleWindow(t *testing.T) {
	cases := map[string]chunking.Options{
		"unknown strategy": {Strategy: "semantic", MaxChars: 800, OverlapPercent: 10},
		"auto window changed": {
			Strategy: chunking.StrategyAuto, MaxChars: 400, OverlapPercent: 10,
		},
		"custom window too small": {
			Strategy: chunking.StrategyCustom, MaxChars: 10, OverlapPercent: 0, Separator: chunking.SeparatorNewline,
		},
		"custom overlap too large": {
			Strategy: chunking.StrategyCustom, MaxChars: 800, OverlapPercent: 80, Separator: chunking.SeparatorNewline,
		},
		"custom separator missing": {
			Strategy: chunking.StrategyCustom, MaxChars: 800, OverlapPercent: 10,
		},
	}

	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := chunking.Split("正文", options); err == nil {
				t.Fatalf("Split(%+v) succeeded, want a rejection", options)
			}
		})
	}
}

// tail returns the last n characters of text, for readable failure output
// without dumping a whole chunk.
func tail(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[len(runes)-n:])
}

// joinTexts concatenates every chunk, so a test can assert that no source text
// went missing.
func joinTexts(result chunking.Result) string {
	var builder strings.Builder
	for _, chunk := range result.Chunks {
		builder.WriteString(chunk.Text)
	}
	return builder.String()
}
