// Package materials owns teaching material: class-scoped reads, safe uploads
// with text extraction, and authorised downloads.
package materials

import (
	"time"

	"campusclaw/internal/httpx"
)

// Material is the metadata shape returned to clients. The stored filename and
// any disk path are deliberately absent: those never leave the server.
type Material struct {
	ID               httpx.ID  `json:"id"`
	ClassID          httpx.ID  `json:"class_id"`
	UploadedBy       httpx.ID  `json:"uploaded_by"`
	Title            string    `json:"title"`
	OriginalFilename string    `json:"original_filename"`
	ContentType      string    `json:"content_type"`
	CreatedAt        time.Time `json:"created_at"`
}

// StoredMaterial adds the server-side storage coordinates.
//
// It exists solely so the authorised download path can find the file. It is
// never encoded into a response — Material, which omits the stored name, is
// what every handler writes out.
type StoredMaterial struct {
	Material
	StoredFilename string
}
