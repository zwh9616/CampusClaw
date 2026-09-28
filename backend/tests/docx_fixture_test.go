package tests

import (
	"archive/zip"
	"bytes"
	"fmt"
	"testing"
)

// WordprocessingML namespaces for the two dialects Word writes.
const (
	nsTransitional = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsStrict       = "http://purl.oclc.org/ooxml/wordprocessingml/main"

	relTransitional = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relStrict       = "http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument"

	ctDocumentMain    = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	ctDocumentMacro   = "application/vnd.ms-word.document.macroEnabled.main+xml"
)

// docxOptions describes a package to assemble. The zero value produces a valid
// Transitional document containing bodyXML.
type docxOptions struct {
	// bodyXML is the inner XML of <w:body>.
	bodyXML string
	// strict selects the Strict dialect namespaces.
	strict bool
	// macro marks the main part as macro-enabled.
	macro bool
	// documentTarget overrides the officeDocument relationship target.
	documentTarget string
	// externalMain marks the main document relationship as external.
	externalMain bool
	// omitContentTypes drops [Content_Types].xml.
	omitContentTypes bool
	// corruptDocument replaces word/document.xml with malformed XML.
	corruptDocument bool
	// extraParts are added verbatim, keyed by zip path.
	extraParts map[string]string
	// entryPad adds this many filler entries.
	entryPad int
	// duplicatePart writes word/document.xml a second time.
	duplicatePart bool
	// traversalPart adds an entry whose name escapes the package.
	traversalPart bool
}

func buildDOCX(t *testing.T, options docxOptions) []byte {
	t.Helper()

	namespace := nsTransitional
	relationship := relTransitional
	if options.strict {
		namespace = nsStrict
		relationship = relStrict
	}

	target := options.documentTarget
	if target == "" {
		target = "word/document.xml"
	}

	targetMode := ""
	if options.externalMain {
		targetMode = ` TargetMode="External"`
	}

	contentType := ctDocumentMain
	if options.macro {
		contentType = ctDocumentMacro
	}

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="` + contentType + `"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="` + relationship + `" Target="` + target + `"` + targetMode + `/>
</Relationships>`

	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="` + namespace + `">
  <w:body>` + options.bodyXML + `</w:body>
</w:document>`

	if options.corruptDocument {
		document = `<?xml version="1.0" encoding="UTF-8"?><w:document><w:body><w:p>`
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	add := func(name, content string) {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}

	if !options.omitContentTypes {
		add("[Content_Types].xml", contentTypes)
	}
	add("_rels/.rels", rels)
	add("word/document.xml", document)

	for name, content := range options.extraParts {
		add(name, content)
	}

	if options.duplicatePart {
		add("word/document.xml", document)
	}

	if options.traversalPart {
		add("../../escaped.xml", "escaped")
	}

	for i := 0; i < options.entryPad; i++ {
		add(fmt.Sprintf("word/filler%04d.xml", i), "pad")
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buffer.Bytes()
}

// paragraph builds a <w:p> containing one run of text.
func paragraph(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

// tableRow builds a <w:tr> with the given cell texts.
func tableRow(cells ...string) string {
	var body bytes.Buffer
	body.WriteString("<w:tr>")
	for _, cell := range cells {
		body.WriteString("<w:tc>" + paragraph(cell) + "</w:tc>")
	}
	body.WriteString("</w:tr>")
	return body.String()
}

// plainZIP builds an ordinary ZIP archive, used to prove that any old ZIP is
// not accepted as a DOCX.
func plainZIP(t *testing.T, entries map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buffer.Bytes()
}

// oleContainer fakes the CFB/OLE header that both an encrypted OOXML package
// and a legacy .doc share.
func oleContainer() []byte {
	header := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	return append(header, bytes.Repeat([]byte{0x00}, 512)...)
}
