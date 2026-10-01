// Package retrieval owns the searchable index: it splits stored teaching text
// into traceable chunks, keeps their vectors in the private vector store, and
// answers keyword, vector, hybrid and evidence-backed question requests.
//
// Two rules shape everything here. MySQL is the only authority for what a chunk
// says, so an excerpt is never taken from the vector store. And every query is
// scoped by the class id taken from the session, so a request can never widen
// its own scope.
package retrieval

import (
	"errors"
	"net/http"

	"campusclaw/internal/httpx"
)

var (
	// ErrNotFound covers a missing row and a row that belongs to another class.
	// Callers map both to the same 404 so nothing discloses which it was.
	ErrNotFound = errors.New("not found")

	// ErrInvalidRequest is a bad query, mode or body the caller must fix.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrUnavailable is a dependency failure: the vector store or a model
	// gateway refused, timed out, or answered with something unusable. It is
	// always reported as a generic retryable error, never with the internal
	// address, body or key that produced it.
	ErrUnavailable = errors.New("retrieval dependency unavailable")

	// ErrForbidden is a request for something the caller's role does not allow.
	ErrForbidden = errors.New("forbidden")
)

// StatusFor maps a retrieval failure onto the fixed HTTP contract.
//
// Anything unrecognised is an internal fault: a caller learns that something
// went wrong, never what the server was doing at the time.
func StatusFor(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, httpx.CodeNotFound
	case errors.Is(err, ErrInvalidRequest):
		return http.StatusBadRequest, httpx.CodeBadRequest
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, httpx.CodeForbidden
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable, httpx.CodeServiceUnavailable
	default:
		return http.StatusInternalServerError, httpx.CodeInternal
	}
}
