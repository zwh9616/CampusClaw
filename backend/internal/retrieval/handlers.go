package retrieval

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/chunking"
	"campusclaw/internal/httpx"
)

// Body limits. A search or a question is a sentence; a conversation carried for
// wording is bounded but larger.
const (
	maxSearchBody = 8 << 10
	maxAskBody    = 64 << 10
	maxIndexBody  = 8 << 10
)

// Handlers serves the retrieval endpoints.
//
// Every handler resolves the class from the session and never from the request,
// so a class id placed in a body, a query string or a header has no effect.
type Handlers struct {
	service *Service
}

// NewHandlers builds the retrieval handlers.
func NewHandlers(service *Service) *Handlers {
	return &Handlers{service: service}
}

type searchRequest struct {
	Query string `json:"query"`
	Mode  string `json:"mode"`
}

// Search answers a keyword, vector or hybrid query within the caller's class.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	var request searchRequest
	if err := decodeBody(w, r, maxSearchBody, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	mode, err := ParseMode(request.Mode)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	result, err := h.service.Search(r.Context(), uint64(user.ClassID), request.Query, mode)
	if err != nil {
		h.writeFailure(w, "search", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, result)
}

type askRequest struct {
	Question string        `json:"question"`
	History  []ChatMessage `json:"history"`
}

// Ask answers a question from the caller's class material, or not at all.
func (h *Handlers) Ask(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	var request askRequest
	if err := decodeBody(w, r, maxAskBody, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	answer, err := h.service.Ask(r.Context(), uint64(user.ClassID), AskRequest{
		Question: request.Question,
		History:  request.History,
	})
	if err != nil {
		h.writeFailure(w, "ask", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, answer)
}

// reindexRequest mirrors the chunk fields an upload accepts, as JSON.
type reindexRequest struct {
	ChunkStrategy       string `json:"chunk_strategy"`
	ChunkMaxChars       *int   `json:"chunk_max_chars"`
	ChunkOverlapPercent *int   `json:"chunk_overlap_percent"`
	ChunkSeparator      string `json:"chunk_separator"`
	RemoveURLsEmails    *bool  `json:"remove_urls_emails"`
	FoldWhitespace      *bool  `json:"fold_whitespace"`
}

type indexResponse struct {
	Index indexJSON `json:"index"`
}

type indexJSON struct {
	Status           string `json:"status"`
	Generation       int    `json:"generation"`
	Strategy         string `json:"strategy"`
	MaxChars         int    `json:"chunk_max_chars"`
	OverlapPercent   int    `json:"chunk_overlap_percent"`
	Separator        string `json:"chunk_separator"`
	RemoveURLsEmails bool   `json:"remove_urls_emails"`
	FoldWhitespace   bool   `json:"fold_whitespace"`
	FailureCode      string `json:"failure_code"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

// Reindex rebuilds one of the class's materials with a chosen strategy.
//
// Only a teacher may reach it, and only for a material already in their class:
// another class's material is answered exactly like one that does not exist.
func (h *Handlers) Reindex(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	materialID, err := httpx.ParseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	}

	var request reindexRequest
	if err := decodeBody(w, r, maxIndexBody, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	options, err := chunking.ParseRequest(request.chunkRequest())
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	state, err := h.service.Reindex(r.Context(), uint64(user.ClassID), uint64(materialID), options)
	if err != nil {
		h.writeFailure(w, "reindex", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, indexResponse{Index: presentIndex(state)})
}

// IndexState reports how one of the class's materials is indexed.
func (h *Handlers) IndexState(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	materialID, err := httpx.ParseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
		return
	}

	state, err := h.service.IndexState(r.Context(), uint64(user.ClassID), uint64(materialID))
	if err != nil {
		h.writeFailure(w, "index state", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, indexResponse{Index: presentIndex(state)})
}

// chunkRequest converts the JSON body into the split request the chunker
// validates, so the API has exactly one place where split parameters are
// interpreted.
func (request reindexRequest) chunkRequest() chunking.Request {
	converted := chunking.Request{
		Strategy:  request.ChunkStrategy,
		Separator: request.ChunkSeparator,
	}

	if request.ChunkMaxChars != nil {
		converted.MaxChars = strconv.Itoa(*request.ChunkMaxChars)
	}
	if request.ChunkOverlapPercent != nil {
		converted.OverlapPercent = strconv.Itoa(*request.ChunkOverlapPercent)
	}
	if request.RemoveURLsEmails != nil {
		converted.RemoveURLsEmails = strconv.FormatBool(*request.RemoveURLsEmails)
	}
	if request.FoldWhitespace != nil {
		converted.FoldWhitespace = strconv.FormatBool(*request.FoldWhitespace)
	}

	return converted
}

func presentIndex(state IndexState) indexJSON {
	presented := indexJSON{
		Status:           state.Status,
		Generation:       state.Generation,
		Strategy:         string(state.Strategy),
		MaxChars:         state.MaxChars,
		OverlapPercent:   state.OverlapPercent,
		Separator:        string(state.Separator),
		RemoveURLsEmails: state.RemoveURLsEmails,
		FoldWhitespace:   state.FoldWhitespace,
		FailureCode:      state.FailureCode,
	}

	if !state.UpdatedAt.IsZero() {
		presented.UpdatedAt = state.UpdatedAt.UTC().Format(time.RFC3339)
	}

	return presented
}

// writeFailure maps a service error onto the fixed error contract.
//
// An unrecognised failure is logged for the operator and answered generically,
// so a response never describes what the server was doing.
func (h *Handlers) writeFailure(w http.ResponseWriter, operation string, err error) {
	status, code := StatusFor(err)

	if status == http.StatusInternalServerError {
		log.Printf("retrieval: %s failed: %v", operation, err)
	}

	httpx.WriteError(w, status, code)
}

// decodeBody reads a bounded JSON body, ignoring fields the API does not use.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)

	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		return errors.New("invalid request body")
	}

	return nil
}
