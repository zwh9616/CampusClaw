package materials

import (
	"context"
	"fmt"
	"os"
)

// Extractor turns a stored original into plain text.
type Extractor interface {
	// Extract reads the file at path — always an absolute, server-generated
	// location — and returns its text.
	Extract(ctx context.Context, path string) (string, error)
}

// TextExtractor serves .md and .txt, whose stored bytes already are the text.
type TextExtractor struct{}

// Extract returns the file's own contents after re-validating them.
func (TextExtractor) Extract(_ context.Context, path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read text upload: %w", err)
	}

	if err := ValidateTextContent(content); err != nil {
		return "", err
	}

	return string(content), nil
}
