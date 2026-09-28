package main

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
)

// This file builds the documents the acceptance run uploads. They are produced
// here rather than committed as binaries so the expected text is visible next
// to the assertion that checks it.

type pdfObject struct {
	number int
	body   string
}

// assemblePDF writes the objects and a correct cross-reference table, so the
// result is a structurally valid PDF rather than something a reader has to
// repair.
func assemblePDF(objects []pdfObject, root int) []byte {
	var out bytes.Buffer

	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

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

// buildPDF produces a one-page PDF with ASCII text plus Chinese text drawn
// through the standard STSong-Light CID font. Resolving the predefined
// UniGB-UCS2-H CMap is the CJK path the extractor has to handle.
func buildPDF(latinText, chineseText string) []byte {
	var encoded strings.Builder
	for _, r := range chineseText {
		if r > 0xFFFF {
			continue
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

// buildScannedPDF produces a page with no text operators, standing in for a
// scanned document.
func buildScannedPDF() []byte {
	return assemblePDF([]pdfObject{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << >> /Contents 4 0 R >>"},
		{4, streamObject("0.5 g 100 600 300 100 re f")},
	}, 1)
}

const (
	nsTransitional = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsStrict       = "http://purl.oclc.org/ooxml/wordprocessingml/main"

	relTransitional = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relStrict       = "http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument"

	contentTypeDocument = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
)

// paragraph renders a <w:p> holding one run of text.
func paragraph(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func tableRow(cells ...string) string {
	var body strings.Builder
	body.WriteString("<w:tr>")
	for _, cell := range cells {
		body.WriteString("<w:tc>" + paragraph(cell) + "</w:tc>")
	}
	body.WriteString("</w:tr>")
	return body.String()
}

// buildDOCX assembles a WordprocessingML package. strict selects the Strict
// dialect namespaces.
func buildDOCX(strict bool, bodyXML string) []byte {
	namespace := nsTransitional
	relationship := relTransitional
	if strict {
		namespace = nsStrict
		relationship = relStrict
	}

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="` + contentTypeDocument + `"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="` + relationship + `" Target="word/document.xml"/>
</Relationships>`

	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="` + namespace + `">
  <w:body>` + bodyXML + `</w:body>
</w:document>`

	return zipPackage(map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels":         rels,
		"word/document.xml":   document,
	})
}

// buildCaptionDOCX is the package the acceptance run uploads: paragraphs plus a
// table, in the dialect requested.
func buildCaptionDOCX(strict bool) []byte {
	return buildDOCX(strict, paragraph("Acceptance paragraph")+
		paragraph("验收段落")+
		"<w:tbl>"+tableRow("CellA1", "CellB1")+tableRow("CellA2", "CellB2")+"</w:tbl>")
}

// zipPackage builds an ordinary ZIP, used both for valid packages and for the
// "plain zip renamed to .docx" rejection case.
func zipPackage(entries map[string]string) []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			panic(err)
		}
	}

	if err := writer.Close(); err != nil {
		panic(err)
	}

	return buffer.Bytes()
}

// buildPlainZIP is a ZIP archive that is not an OOXML package at all.
func buildPlainZIP() []byte {
	return zipPackage(map[string]string{"notes.txt": "just a zip, not a document"})
}

// buildOLEFile fakes the CFB/OLE header shared by an encrypted OOXML package
// and a legacy .doc.
func buildOLEFile() []byte {
	return append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1},
		bytes.Repeat([]byte{0x00}, 512)...)
}

// buildImageOnlyDOCX is a valid package whose body has no text.
func buildImageOnlyDOCX() []byte {
	return buildDOCX(false, "")
}
