package materials

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// PDF limits. Pages and output size are 413; a parse timeout is 422.
const (
	maxPDFPages   = 200
	maxPDFOutput  = 5 << 20
	maxPDFStderr  = 64 << 10
	pdferrTooBig  = "output exceeded the limit"
)

// PDFExtractor reads the text layer of a PDF using the Poppler tools that ship
// inside the API image.
//
// Both commands are invoked directly with explicit arguments. No shell is
// involved, so a filename can never be interpreted as a command, and the path
// passed is always the server-generated stored location.
type PDFExtractor struct {
	// InfoBin and TextBin allow the binary names to be overridden; the
	// defaults match the packaged poppler-utils.
	InfoBin string
	TextBin string
	// Timeout bounds metadata reading plus extraction together.
	Timeout time.Duration
}

// NewPDFExtractor returns an extractor using the packaged Poppler commands.
func NewPDFExtractor() PDFExtractor {
	return PDFExtractor{InfoBin: "pdfinfo", TextBin: "pdftotext"}
}

// Extract returns the document's text layer.
func (e PDFExtractor) Extract(ctx context.Context, filePath string) (string, error) {
	infoBin := e.InfoBin
	if infoBin == "" {
		infoBin = "pdfinfo"
	}
	textBin := e.TextBin
	if textBin == "" {
		textBin = "pdftotext"
	}

	if _, err := exec.LookPath(infoBin); err != nil {
		return "", fmt.Errorf("pdf parser %s is not installed: %w", infoBin, err)
	}
	if _, err := exec.LookPath(textBin); err != nil {
		return "", fmt.Errorf("pdf parser %s is not installed: %w", textBin, err)
	}

	timeout := e.Timeout
	if timeout <= 0 {
		timeout = DefaultParseTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	info, err := runBounded(ctx, infoBin, []string{filePath}, maxPDFStderr)
	if err != nil {
		return "", err
	}

	if err := checkPDFInfo(string(info)); err != nil {
		return "", err
	}

	// -enc UTF-8 and -eol unix make the output deterministic; -nopgbrk keeps
	// form feeds out of the text. No password flag is ever passed.
	raw, err := runBounded(ctx, textBin,
		[]string{"-enc", "UTF-8", "-eol", "unix", "-nopgbrk", filePath, "-"},
		maxPDFOutput,
	)
	if err != nil {
		return "", err
	}

	text := normaliseExtractedText(string(raw))

	// A scanned or image-only PDF yields nothing usable. This is reported,
	// never worked around with OCR.
	if err := ValidateExtractedText(text); err != nil {
		return "", err
	}

	return text, nil
}

// VerifyParserTools reports whether the packaged document parsers are present.
//
// The API refuses to start without them: a missing tool must surface as a
// deployment error, never as every uploaded PDF looking corrupt.
func VerifyParserTools() error {
	for _, name := range []string{"pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("required document parser %q is not installed", name)
		}
	}

	return nil
}

// checkPDFInfo rejects encrypted documents and documents over the page limit.
func checkPDFInfo(info string) error {
	var encrypted bool
	var pages int
	var sawPages bool

	for _, line := range strings.Split(info, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)

		switch key {
		case "encrypted":
			encrypted = strings.HasPrefix(strings.ToLower(value), "yes")
		case "pages":
			count, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%w: unreadable page count", ErrInvalidFile)
			}
			pages = count
			sawPages = true
		}
	}

	if encrypted {
		return fmt.Errorf("%w: the document is encrypted", ErrUnprocessable)
	}

	if !sawPages {
		return fmt.Errorf("%w: the document has no readable page structure", ErrInvalidFile)
	}

	if pages > maxPDFPages {
		return fmt.Errorf("%w: %d pages exceeds the limit of %d", ErrTooLarge, pages, maxPDFPages)
	}

	return nil
}

// runBounded executes a command, capturing at most limit bytes of stdout and
// the same order of magnitude of stderr. Over-limit stdout is a classification,
// not a crash: the process is killed and reaped before returning.
func runBounded(ctx context.Context, name string, args []string, limit int) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)

	var stderr boundedBuffer
	stderr.limit = maxPDFStderr
	command.Stderr = &stderr

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	if err := command.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("pdf parser %s is not installed: %w", name, err)
		}
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	content, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))

	if int64(len(content)) > int64(limit) {
		// The tool produced more than we will ever store; stop it and reap it.
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, pdferrTooBig)
	}

	waitErr := command.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("%w: the document took too long to parse", ErrUnprocessable)
	}

	if readErr != nil {
		return nil, fmt.Errorf("%w: reading %s output failed", ErrInvalidFile, name)
	}

	if waitErr != nil {
		// stderr is kept for classification only; it is never returned to the
		// caller nor written to the response.
		return nil, fmt.Errorf("%w: %s could not read the document", ErrInvalidFile, name)
	}

	return content, nil
}

// boundedBuffer keeps at most limit bytes and silently drops the rest, so a
// noisy parser cannot exhaust memory.
type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if remaining := b.limit - b.buffer.Len(); remaining > 0 {
		if len(p) > remaining {
			b.buffer.Write(p[:remaining])
		} else {
			b.buffer.Write(p)
		}
	}
	return len(p), nil
}

var _ Extractor = PDFExtractor{}
