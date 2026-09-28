package materials

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// WordprocessingML namespaces, in both the Transitional and Strict dialects.
const (
	wordprocessingTransitional = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	wordprocessingStrict       = "http://purl.oclc.org/ooxml/wordprocessingml/main"
)

// Office package plumbing, again in both dialects.
const (
	officeDocumentTransitional = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	officeDocumentStrict       = "http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument"

	contentTypeDocument      = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	contentTypeMacroDocument = "application/vnd.ms-word.document.macroEnabled.main+xml"
)

// Resource ceilings. Exceeding them is a 413; running out of time is a 422.
const (
	docxMaxEntries      = 1000
	docxMaxUncompressed = 20 << 20
	docxMaxTextBytes    = 5 << 20
	docxMaxXMLDepth     = 128
	// DefaultParseTimeout bounds metadata reading plus extraction.
	DefaultParseTimeout = 10 * time.Second
	// tokensBetweenDeadlineChecks keeps the clock check off the hot path.
	tokensBetweenDeadlineChecks = 256
)

// CFB/OLE signature: a legacy .doc, or an encrypted OOXML package, both of
// which are refused the same way rather than decrypted.
var oleSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// DOCXExtractor reads the body text of a WordprocessingML package.
//
// It never shells out, never writes to disk and never fetches a URL: the
// archive is read in memory through archive/zip, and only the main document
// part is parsed, so headers, footers, comments and embedded objects cannot
// leak into the knowledge base.
type DOCXExtractor struct {
	// Timeout bounds the whole parse; zero means DefaultParseTimeout.
	Timeout time.Duration
}

// Extract returns the document's paragraph and table text in document order.
func (e DOCXExtractor) Extract(ctx context.Context, filePath string) (string, error) {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = DefaultParseTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read document: %w", err)
	}

	if bytes.HasPrefix(raw, oleSignature) {
		// Covers both an encrypted OOXML package and a renamed legacy .doc.
		return "", fmt.Errorf("%w: unsupported Office container", ErrUnprocessable)
	}

	if !bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		return "", fmt.Errorf("%w: not an OOXML package", ErrInvalidFile)
	}

	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("%w: unreadable zip container", ErrInvalidFile)
	}

	pkg := &packageReader{archive: archive}

	if err := pkg.checkEnvelopeLimits(); err != nil {
		return "", err
	}

	mainPart, err := pkg.mainDocumentPart(ctx)
	if err != nil {
		return "", err
	}

	document, err := pkg.readBounded(ctx, mainPart)
	if err != nil {
		return "", err
	}

	text, err := extractDocumentText(ctx, document)
	if err != nil {
		return "", err
	}

	if err := ValidateExtractedText(text); err != nil {
		return "", err
	}

	return text, nil
}

// packageReader reads a DOCX package with a cumulative decompression budget.
type packageReader struct {
	archive   *zip.Reader
	decompressed uint64
}

// checkEnvelopeLimits rejects a package by its declared shape before anything
// is decompressed, and refuses part names that are absolute, parent-relative
// or duplicated.
//
// Nothing is ever extracted to disk, so a hostile entry name cannot write
// outside the package; refusing it keeps part resolution unambiguous instead.
func (p *packageReader) checkEnvelopeLimits() error {
	if len(p.archive.File) > docxMaxEntries {
		return fmt.Errorf("%w: %d zip entries exceeds the limit of %d",
			ErrTooLarge, len(p.archive.File), docxMaxEntries)
	}

	seen := make(map[string]bool, len(p.archive.File))
	var declared uint64

	for _, entry := range p.archive.File {
		if seen[entry.Name] {
			return fmt.Errorf("%w: duplicate package part", ErrInvalidFile)
		}
		seen[entry.Name] = true

		if !isSafePartName(entry.Name) {
			return fmt.Errorf("%w: unsafe package part name", ErrInvalidFile)
		}

		declared += entry.UncompressedSize64
	}

	if declared > docxMaxUncompressed {
		return fmt.Errorf("%w: declared decompressed size exceeds %d bytes", ErrTooLarge, docxMaxUncompressed)
	}

	return nil
}

// isSafePartName reports whether a part name stays inside the package.
func isSafePartName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return false
	}

	cleaned := path.Clean(name)
	return cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func (p *packageReader) find(name string) (*zip.File, error) {
	for _, entry := range p.archive.File {
		if entry.Name == name {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("%w: package is missing %s", ErrInvalidFile, name)
}

// readBounded reads one part, counting the bytes actually decompressed against
// the shared budget. The declared sizes are only a hint: a crafted archive can
// lie about them, so the real total is what is enforced.
func (p *packageReader) readBounded(ctx context.Context, name string) ([]byte, error) {
	entry, err := p.find(name)
	if err != nil {
		return nil, err
	}

	handle, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open %s", ErrInvalidFile, name)
	}
	defer handle.Close()

	remaining := docxMaxUncompressed - p.decompressed
	limited := io.LimitReader(handle, int64(remaining)+1)

	content, err := io.ReadAll(limited)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%w: parsing timed out", ErrUnprocessable)
		}
		// A CRC mismatch surfaces here, which is exactly the corruption we
		// want reported as a malformed document.
		return nil, fmt.Errorf("%w: reading %s failed: %v", ErrInvalidFile, name, err)
	}

	if uint64(len(content)) > remaining {
		return nil, fmt.Errorf("%w: decompressed content exceeds %d bytes", ErrTooLarge, docxMaxUncompressed)
	}

	p.decompressed += uint64(len(content))

	return content, nil
}

// contentTypes maps each part to its declared content type.
type contentTypes struct {
	XMLName   xml.Name `xml:"Types"`
	Overrides []struct {
		PartName    string `xml:"PartName,attr"`
		ContentType string `xml:"ContentType,attr"`
	} `xml:"Override"`
}

// relationships is _rels/.rels.
type relationships struct {
	XMLName xml.Name `xml:"Relationships"`
	Items   []struct {
		Type         string `xml:"Type,attr"`
		Target       string `xml:"Target,attr"`
		TargetMode   string `xml:"TargetMode,attr"`
		Relationship string `xml:"Id,attr"`
	} `xml:"Relationship"`
}

// mainDocumentPart resolves the officeDocument relationship rather than
// assuming word/document.xml, and validates the part it points at.
func (p *packageReader) mainDocumentPart(ctx context.Context) (string, error) {
	typesRaw, err := p.readBounded(ctx, "[Content_Types].xml")
	if err != nil {
		return "", err
	}

	var types contentTypes
	if err := decodeXML(typesRaw, &types); err != nil {
		return "", fmt.Errorf("%w: [Content_Types].xml is malformed", ErrInvalidFile)
	}

	relsRaw, err := p.readBounded(ctx, "_rels/.rels")
	if err != nil {
		return "", err
	}

	var rels relationships
	if err := decodeXML(relsRaw, &rels); err != nil {
		return "", fmt.Errorf("%w: _rels/.rels is malformed", ErrInvalidFile)
	}

	var target string
	for _, rel := range rels.Items {
		if !strings.HasSuffix(rel.Type, "/officeDocument") {
			continue
		}

		// An external main document would be a request to fetch something.
		if strings.EqualFold(rel.TargetMode, "External") {
			return "", fmt.Errorf("%w: the main document is an external target", ErrInvalidFile)
		}

		target = rel.Target
		break
	}

	if target == "" {
		return "", fmt.Errorf("%w: no officeDocument relationship found", ErrInvalidFile)
	}

	part, err := resolvePartPath(target)
	if err != nil {
		return "", err
	}

	contentType := ""
	for _, override := range types.Overrides {
		if strings.EqualFold(strings.TrimPrefix(override.PartName, "/"), part) {
			contentType = override.ContentType
			break
		}
	}

	switch strings.ToLower(contentType) {
	case contentTypeDocument:
		// Expected.
	case contentTypeMacroDocument:
		return "", fmt.Errorf("%w: the document contains macros", ErrInvalidFile)
	default:
		return "", fmt.Errorf("%w: %s is not a WordprocessingML document part", ErrInvalidFile, part)
	}

	return part, nil
}

// resolvePartPath normalises a relationship target and refuses anything that
// would escape the package.
func resolvePartPath(target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("%w: empty relationship target", ErrInvalidFile)
	}

	if strings.Contains(target, "://") || strings.HasPrefix(target, "//") {
		return "", fmt.Errorf("%w: relationship target is an external URL", ErrInvalidFile)
	}

	cleaned := path.Clean(strings.TrimPrefix(target, "/"))

	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: relationship target escapes the package", ErrInvalidFile)
	}

	return cleaned, nil
}

// decodeXML parses a part into target, first refusing DTDs and custom entities
// so a hostile document cannot expand entities or pull in external content.
//
// The scan is a separate pass because xml.Unmarshal silently skips directives
// rather than rejecting them.
func decodeXML(raw []byte, target any) error {
	if err := rejectDirectives(raw); err != nil {
		return err
	}

	return xml.Unmarshal(raw, target)
}

// rejectDirectives fails on any <!...> directive, which in practice means a
// DOCTYPE, the usual vector for entity expansion.
func rejectDirectives(raw []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = true

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if _, isDirective := token.(xml.Directive); isDirective {
			return errors.New("XML directives are not allowed")
		}
	}
}

// documentBody converts the main part into plain text, in document order.
//
// Collecting only w:t and the explicit w:tab / w:br markers is what keeps
// deleted revisions (w:delText), field codes (w:instrText), images and
// embedded objects out of the result: none of them carry a w:t.
func extractDocumentText(ctx context.Context, raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = true
	// OOXML uses UTF-8; anything else is not a document we accept.
	decoder.CharsetReader = nil

	var (
		output   []byte
		depth    int
		tokens   int
		inBody   bool
		textSize int
	)

	flush := func(fragment string) error {
		textSize += len(fragment)
		if textSize > docxMaxTextBytes {
			return fmt.Errorf("%w: extracted text exceeds %d bytes", ErrTooLarge, docxMaxTextBytes)
		}
		output = append(output, fragment...)
		return nil
	}

	// closeCell turns the paragraph break that ended a cell into the tab that
	// separates it from the next cell, so a cell's own paragraphs stay inside
	// it instead of each one looking like a new row.
	closeCell := func() error {
		if len(output) > 0 && output[len(output)-1] == '\n' {
			output[len(output)-1] = '\t'
			return nil
		}
		return flush("\t")
	}

	for {
		tokens++
		if tokens%tokensBetweenDeadlineChecks == 0 {
			if err := ctx.Err(); err != nil {
				return "", fmt.Errorf("%w: parsing timed out", ErrUnprocessable)
			}
		}

		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("%w: parsing timed out", ErrUnprocessable)
			}
			return "", fmt.Errorf("%w: malformed document XML", ErrInvalidFile)
		}

		switch element := token.(type) {
		case xml.Directive:
			return "", fmt.Errorf("%w: the document contains an XML directive", ErrInvalidFile)

		case xml.StartElement:
			depth++
			if depth > docxMaxXMLDepth {
				return "", fmt.Errorf("%w: XML nesting exceeds %d", ErrTooLarge, docxMaxXMLDepth)
			}

			if !isWordprocessing(element.Name) {
				continue
			}

			switch element.Name.Local {
			case "body":
				inBody = true
			case "t":
				if !inBody {
					continue
				}
				text, err := readElementText(decoder)
				if err != nil {
					return "", err
				}
				if err := flush(text); err != nil {
					return "", err
				}
			case "tab":
				if inBody {
					if err := flush("\t"); err != nil {
						return "", err
					}
				}
			case "br", "cr":
				if inBody {
					if err := flush("\n"); err != nil {
						return "", err
					}
				}
			}

		case xml.EndElement:
			depth--
			if depth < 0 {
				return "", fmt.Errorf("%w: unbalanced document XML", ErrInvalidFile)
			}

			if !isWordprocessing(element.Name) || !inBody {
				continue
			}

			switch element.Name.Local {
			case "body":
				inBody = false
			case "p":
				if err := flush("\n"); err != nil {
					return "", err
				}
			case "tc":
				if err := closeCell(); err != nil {
					return "", err
				}
			case "tr":
				if err := flush("\n"); err != nil {
					return "", err
				}
			}
		}
	}

	return normaliseExtractedText(string(output)), nil
}

// readElementText returns the character data up to the end of the current
// element, ignoring any nested markup.
func readElementText(decoder *xml.Decoder) (string, error) {
	var (
		builder strings.Builder
		depth   int
	)

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", fmt.Errorf("%w: truncated document XML", ErrInvalidFile)
			}
			return "", fmt.Errorf("%w: malformed document XML", ErrInvalidFile)
		}

		switch typed := token.(type) {
		case xml.CharData:
			builder.Write(typed)
		case xml.StartElement:
			depth++
			if depth > docxMaxXMLDepth {
				return "", fmt.Errorf("%w: XML nesting exceeds %d", ErrTooLarge, docxMaxXMLDepth)
			}
		case xml.EndElement:
			if depth == 0 {
				return builder.String(), nil
			}
			depth--
		}
	}
}

func isWordprocessing(name xml.Name) bool {
	return name.Space == wordprocessingTransitional || name.Space == wordprocessingStrict
}

// normaliseExtractedText makes the output deterministic: LF endings, no
// trailing whitespace on a line, no dangling cell separators, and no runs of
// blank lines.
func normaliseExtractedText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	blankRun := 0

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")

		if line == "" {
			blankRun++
			// At most one blank line survives, so paragraph spacing is kept
			// without producing runs of empty lines.
			if blankRun > 1 {
				continue
			}
		} else {
			blankRun = 0
		}

		cleaned = append(cleaned, line)
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// DOCXExtractor satisfies the Extractor contract.
var _ Extractor = DOCXExtractor{}
