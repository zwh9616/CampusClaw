package materials

import (
	"context"
	"errors"
	"log"
	"net/http"

	"campusclaw/internal/auth"
	"campusclaw/internal/httpx"
)

// Repository is the persistence surface the handlers need. It is an interface
// so handler behaviour can be exercised without a database; the production
// implementation is *Repo.
//
// Every read takes the caller's class id explicitly — there is no
// lookup-by-primary-key method a handler could call by mistake.
type Repository interface {
	List(ctx context.Context, classID httpx.ID) ([]Material, error)
	Detail(ctx context.Context, id, classID httpx.ID) (Material, string, error)
	Metadata(ctx context.Context, id, classID httpx.ID) (Material, error)
	StoredMetadata(ctx context.Context, id, classID httpx.ID) (StoredMaterial, error)
	Create(ctx context.Context, input NewMaterial) (Material, error)
}

// Handlers serves the material endpoints.
type Handlers struct {
	repo       Repository
	storage    *Storage
	extractors map[string]Extractor
	limiter    *ParseLimiter
}

// NewHandlers builds the material handlers. extractors is keyed by lowercase
// extension, e.g. ".md".
func NewHandlers(
	repo Repository,
	storage *Storage,
	extractors map[string]Extractor,
	limiter *ParseLimiter,
) *Handlers {
	return &Handlers{repo: repo, storage: storage, extractors: extractors, limiter: limiter}
}

type listResponse struct {
	Materials []Material `json:"materials"`
}

type detailResponse struct {
	Material Material `json:"material"`
	Content  string   `json:"content"`
}

// List returns the current user's class material.
//
// The class comes from the session and nowhere else: query parameters such as
// class_id are not read, so they cannot widen the result set.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	materials, err := h.repo.List(r.Context(), user.ClassID)
	if err != nil {
		log.Printf("materials: list failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, listResponse{Materials: materials})
}

// Detail returns one material with its extracted text.
func (h *Handlers) Detail(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	// An unusable id is answered exactly like a missing row: same status, same
	// body. Nothing here reveals whether the resource exists.
	id, err := httpx.ParseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	}

	material, content, err := h.repo.Detail(r.Context(), id, user.ClassID)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	case err != nil:
		log.Printf("materials: detail failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, detailResponse{Material: material, Content: content})
}
