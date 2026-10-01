package chunking

import "unicode"

// splitWindows slides a window of options.MaxChars over the text.
//
// Each window backs off to the last preferred break so a chunk ends at a blank
// line, a newline or a full stop rather than mid-sentence, and when the window
// holds no break at all it is cut at the limit. Consecutive windows overlap by
// options.overlapChars() so a sentence spanning a boundary is still whole in
// one of them.
func splitWindows(runes []rune, options Options) []Chunk {
	total := len(runes)
	if total == 0 {
		return nil
	}

	markers := options.markers()
	overlap := options.overlapChars()
	minFill := options.MaxChars / 2
	chunks := make([]Chunk, 0, total/windowStep(options)+1)

	for start := 0; start < total; {
		end := start + options.MaxChars
		if end >= total {
			end = total
		} else {
			end = breakPoint(runes, start, end, markers, minFill)
		}

		// The emitted range is the trimmed window, so Text is exactly
		// runes[Start:End] and a reader can check the range by slicing.
		chunkStart, chunkEnd := trimRange(runes, start, end)
		if chunkStart < chunkEnd {
			chunks = append(chunks, Chunk{
				Index: len(chunks),
				Text:  string(runes[chunkStart:chunkEnd]),
				Start: chunkStart,
				End:   chunkEnd,
			})
		}

		if end >= total {
			break
		}

		// Step back by the overlap. The guard keeps the window moving forward
		// even if a break landed immediately after the previous start.
		next := end - overlap
		if next <= start {
			next = start + 1
		}
		start = next
	}

	return chunks
}

// breakPoint returns the end of a window after backing off to the last
// preferred break, or the limit itself when there is none.
//
// Markers are tried in priority order, so a blank line anywhere in the window
// wins over a nearer newline; within one marker the latest occurrence wins, so
// the window stays as full as possible.
//
// A break only counts when it keeps at least minFill characters. Without that
// rule a chapter heading followed by one very long line would break at the
// heading and emit a handful of characters, which is worse for retrieval than
// cutting the line at the limit.
func breakPoint(runes []rune, start, limit int, markers [][]rune, minFill int) int {
	for _, marker := range markers {
		for cut := limit; cut-start >= minFill; cut-- {
			if matchesAt(runes, cut-len(marker), marker) {
				return cut
			}
		}
	}

	return limit
}

// matchesAt reports whether marker sits at position at.
func matchesAt(runes []rune, at int, marker []rune) bool {
	if at < 0 || at+len(marker) > len(runes) {
		return false
	}

	for offset, r := range marker {
		if runes[at+offset] != r {
			return false
		}
	}

	return true
}

// trimRange narrows a window past the whitespace at either edge, so a chunk
// never starts or ends with padding that a break happened to leave behind.
func trimRange(runes []rune, start, end int) (int, int) {
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}

	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}

	return start, end
}

// windowStep is how far the window advances between chunks.
func windowStep(options Options) int {
	step := options.MaxChars - options.overlapChars()
	if step < 1 {
		step = 1
	}
	return step
}

// section is one Markdown chapter: its heading line and everything up to the
// next heading.
type section struct {
	start int
	end   int
}

// splitHierarchy keeps each Markdown chapter whole and falls back to the window
// rules for a chapter longer than the window.
//
// The heading line is part of its section, so the text a reader sees always
// says which chapter it came from. The returned ranges are shifted back into
// the whole document.
func splitHierarchy(runes []rune, options Options) []Chunk {
	if len(runes) == 0 {
		return nil
	}

	var chunks []Chunk

	for _, chapter := range headingSections(runes) {
		body := runes[chapter.start:chapter.end]

		if len(body) <= options.MaxChars {
			start, end := trimRange(runes, chapter.start, chapter.end)
			if start < end {
				chunks = append(chunks, Chunk{
					Index: len(chunks),
					Text:  string(runes[start:end]),
					Start: start,
					End:   end,
				})
			}
			continue
		}

		for _, chunk := range splitWindows(body, options) {
			chunks = append(chunks, Chunk{
				Index: len(chunks),
				Text:  chunk.Text,
				Start: chunk.Start + chapter.start,
				End:   chunk.End + chapter.start,
			})
		}
	}

	return chunks
}

// headingSections lists the chapters of a Markdown document.
//
// A chapter begins at a line that opens with one to three hashes followed by a
// space. Anything before the first heading is its own section rather than being
// dropped, so no text can go missing from the index.
func headingSections(runes []rune) []section {
	var starts []int

	for i := 0; i < len(runes); {
		lineStart := i

		for i < len(runes) && runes[i] != '\n' {
			i++
		}

		if isHeading(runes[lineStart:i]) {
			starts = append(starts, lineStart)
		}

		if i < len(runes) {
			i++ // step over the line break
		}
	}

	if len(starts) == 0 {
		return []section{{start: 0, end: len(runes)}}
	}

	sections := make([]section, 0, len(starts)+1)

	if starts[0] > 0 {
		sections = append(sections, section{start: 0, end: starts[0]})
	}

	for index, start := range starts {
		end := len(runes)
		if index+1 < len(starts) {
			end = starts[index+1]
		}
		sections = append(sections, section{start: start, end: end})
	}

	return sections
}

// isHeading reports whether a line opens a Markdown heading of level one to
// three. A fourth hash is deliberately not a heading, matching KR-01.
func isHeading(line []rune) bool {
	hashes := 0
	for hashes < len(line) && line[hashes] == '#' {
		hashes++
	}

	if hashes < 1 || hashes > 3 {
		return false
	}

	return hashes < len(line) && line[hashes] == ' '
}
