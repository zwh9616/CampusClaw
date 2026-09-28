package materials

import (
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"

	"campusclaw/internal/auth"
	"campusclaw/internal/httpx"
)

// Download serves the original stored file.
//
// The order matters and is enforced structurally: authenticate, then resolve
// the material by id *and* the session's class, and only then touch the
// filesystem. An unauthorised request never reaches an open().
func (h *Handlers) Download(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	// An unusable id is answered exactly like a missing row.
	id, err := httpx.ParseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	}

	material, err := h.repo.StoredMetadata(r.Context(), id, user.ClassID)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	case err != nil:
		log.Printf("materials: download lookup failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	// Authorised. A missing or unreadable original is indistinguishable from
	// "no such material" to the caller.
	file, err := h.storage.Open(user.ClassID, material.StoredFilename)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	}

	w.Header().Set("Content-Type", material.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", disposition(material.OriginalFilename))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		// The status line is already sent; all that is left is to record it.
		log.Printf("materials: streaming download failed: %v", err)
	}
}

// disposition builds an attachment header from the sanitized original name.
// mime.FormatMediaType applies RFC 5987 encoding when the name is not ASCII,
// so a Chinese filename survives without breaking the header.
func disposition(originalFilename string) string {
	formatted := mime.FormatMediaType("attachment", map[string]string{
		"filename": SanitizeFilename(originalFilename),
	})

	if formatted == "" {
		return "attachment"
	}

	return formatted
}
