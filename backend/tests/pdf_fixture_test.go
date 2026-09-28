package tests

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// pdfObject is one indirect object in the PDF we assemble.
type pdfObject struct {
	number int
	body   string
}

// assemblePDF writes the objects out and appends a correct cross-reference
// table, which is what makes the result a structurally valid PDF rather than
// something Poppler has to guess at.
func assemblePDF(objects []pdfObject, root int) []byte {
	var out bytes.Buffer

	// The second line is a binary comment marking the file as binary, which is
	// conventional and harmless.
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// The table is sized by the highest object number, not the object count,
	// so fixtures that skip a number still produce a well-formed xref.
	highest := 0
	for _, object := range objects {
		if object.number > highest {
			highest = object.number
		}
	}

	offsets := make([]int, highest+1)
	for _, object := range objects {
		offsets[object.number] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", object.number, object.body)
	}

	xrefStart := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", highest+1)
	out.WriteString("0000000000 65535 f \n")
	for i := 1; i <= highest; i++ {
		if offsets[i] == 0 {
			// Unused number: list it as free rather than pointing at byte 0.
			out.WriteString("0000000000 65535 f \n")
			continue
		}
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		highest+1, root, xrefStart)

	return out.Bytes()
}

func streamObject(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

// buildLatinPDF produces a one-page PDF containing only ASCII text.
func buildLatinPDF(text string) []byte {
	content := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", text)

	return assemblePDF([]pdfObject{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>"},
		{4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
		{5, streamObject(content)},
	}, 1)
}

// buildCJKPDF produces a one-page PDF mixing ASCII text with Chinese text drawn
// through the standard STSong-Light CID font and the predefined UniGB-UCS2-H
// CMap. Resolving that CMap is exactly the CJK path the extractor must handle.
func buildCJKPDF(latinText, cjkText string) []byte {
	// Characters are written as UCS-2 code units, which is what UniGB-UCS2-H
	// expects to receive.
	var encoded strings.Builder
	for _, r := range cjkText {
		if r > 0xFFFF {
			continue // outside the BMP; not used by the fixtures
		}
		encoded.WriteString(hex.EncodeToString([]byte{byte(r >> 8), byte(r)}))
	}

	content := fmt.Sprintf(
		"BT /F1 24 Tf 72 700 Td (%s) Tj ET\nBT /F2 24 Tf 72 660 Td <%s> Tj ET",
		latinText, encoded.String(),
	)

	return assemblePDF([]pdfObject{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R /F2 6 0 R >> >> /Contents 5 0 R >>"},
		{4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
		{5, streamObject(content)},
		{6, "<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light " +
			"/Encoding /UniGB-UCS2-H /DescendantFonts [7 0 R] >>"},
		{7, "<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light " +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> >>"},
	}, 1)
}

// buildImageOnlyPDF produces a PDF with no text operators at all, standing in
// for a scanned document.
func buildImageOnlyPDF() []byte {
	// A page whose content stream draws a rectangle: valid PDF, no text layer.
	content := "0.5 g 100 600 300 100 re f"

	return assemblePDF([]pdfObject{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << >> /Contents 4 0 R >>"},
		{4, streamObject(content)},
	}, 1)
}

// buildPaddedPDF produces a PDF long enough to exceed a byte limit, used to
// exercise the output ceiling without generating thousands of pages.
func buildPaddedPDF(fillerBytes int) []byte {
	content := "BT /F1 12 Tf 72 700 Td (" + strings.Repeat("A", fillerBytes) + ") Tj ET"

	return assemblePDF([]pdfObject{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>"},
		{4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
		{5, streamObject(content)},
	}, 1)
}

// buildManyPagePDF produces a PDF declaring more pages than the extractor
// allows, to exercise the page-count ceiling.
func buildManyPagePDF(pages int) []byte {
	var kids strings.Builder
	for i := 3; i < 3+pages; i++ {
		fmt.Fprintf(&kids, "%d 0 R ", i)
	}

	objects := []pdfObject{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), pages)},
	}

	for i := 3; i < 3+pages; i++ {
		objects = append(objects, pdfObject{i, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents %d 0 R >>",
			3+pages+(i-3))})
	}
	for i := 3; i < 3+pages; i++ {
		objects = append(objects, pdfObject{3 + pages + (i - 3), streamObject("")})
	}

	return assemblePDF(objects, 1)
}

func TestPDFFixtureExtraction(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	// Probe: confirm the hand-built fixtures are actually readable by the
	// packaged Poppler, and see what text comes back.
	extractor := newPDFExtractorForTest()

	dir := t.TempDir()

	latin := writeFixture(t, dir, "latin.pdf", buildLatinPDF("Hello CampusClaw"))
	text, err := extractor.Extract(t.Context(), latin)
	if err != nil {
		t.Fatalf("latin PDF: %v", err)
	}
	if !strings.Contains(text, "Hello CampusClaw") {
		t.Errorf("latin PDF text = %q, want it to contain the drawn string", text)
	}

	cjk := writeFixture(t, dir, "cjk.pdf", buildCJKPDF("Greeting", "你好世界"))
	text, err = extractor.Extract(t.Context(), cjk)
	if err != nil {
		t.Fatalf("CJK PDF: %v", err)
	}
	t.Logf("CJK extraction result: %q", text)

	if !strings.Contains(text, "你好世界") {
		t.Errorf("CJK PDF text = %q, want it to contain 你好世界", text)
	}
}
