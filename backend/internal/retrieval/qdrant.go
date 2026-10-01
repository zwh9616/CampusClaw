package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CollectionName is the single collection every class shares. Tenancy is a
// payload filter, not a collection per class: a collection would have to be
// created and dropped alongside a class, and a filter is checked on every
// query anyway.
const CollectionName = "campusclaw_chunks"

// Payload keys. Every value is a decimal string: a class id is a BIGINT
// UNSIGNED, and a JSON number could not carry it exactly through every client.
const (
	payloadClassID          = "class_id"
	payloadMaterialID       = "material_id"
	payloadKnowledgeEntryID = "knowledge_entry_id"
	payloadChunkID          = "chunk_id"
	payloadChunkIndex       = "chunk_index"
)

// maxResponseBody bounds how much of a vector-store response is buffered, so a
// hostile or broken server cannot make the API read without limit. Every
// response this client parses is far smaller than the cap.
const maxResponseBody = 1 << 20

// VectorPoint is one chunk's vector and the identifiers that let a hit be
// resolved back to a row. The chunk text is deliberately absent: it lives in
// MySQL, and a vector store is not an authority on what a source says.
type VectorPoint struct {
	ID               uint64
	Vector           []float32
	ClassID          uint64
	MaterialID       uint64
	KnowledgeEntryID uint64
	ChunkIndex       int
}

// ScoredPoint is one vector hit.
type ScoredPoint struct {
	ID    uint64
	Score float64
}

// QdrantClient talks to the private vector store over its REST API.
type QdrantClient struct {
	base       *url.URL
	collection string
	http       *http.Client
}

// NewQdrant builds a client. The base URL is server-side configuration and is
// never handed to a browser.
func NewQdrant(base *url.URL, collection string, timeout time.Duration) *QdrantClient {
	return &QdrantClient{
		base:       base,
		collection: collection,
		http:       &http.Client{Timeout: timeout},
	}
}

// EnsureCollection creates the collection when it is missing and verifies the
// vector shape when it is not.
//
// A dimension that disagrees with the configuration is an error rather than
// something to paper over: writing a 1536-wide vector into a 512-wide
// collection is rejected by the store, and silently recreating the collection
// would throw away every existing vector.
func (q *QdrantClient) EnsureCollection(ctx context.Context, dimensions int) error {
	status, body, err := q.do(ctx, http.MethodGet, "/collections/"+q.collection, nil)
	if err != nil {
		return err
	}

	switch status {
	case http.StatusOK:
		return checkCollectionShape(body, dimensions)
	case http.StatusNotFound:
		return q.createCollection(ctx, dimensions)
	default:
		return fmt.Errorf("%w: collection lookup answered %d", ErrUnavailable, status)
	}
}

func (q *QdrantClient) createCollection(ctx context.Context, dimensions int) error {
	request := map[string]any{
		"vectors": map[string]any{
			"size":     dimensions,
			"distance": "Cosine",
		},
	}

	status, _, err := q.do(ctx, http.MethodPut, "/collections/"+q.collection, request)
	if err != nil {
		return err
	}

	if status != http.StatusOK {
		return fmt.Errorf("%w: collection creation answered %d", ErrUnavailable, status)
	}

	return nil
}

// collectionShape is the part of a collection description this API depends on.
type collectionShape struct {
	Result struct {
		Config struct {
			Params struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			} `json:"params"`
		} `json:"config"`
	} `json:"result"`
}

// checkCollectionShape proves the existing collection can hold these vectors.
func checkCollectionShape(body []byte, dimensions int) error {
	var shape collectionShape
	if err := json.Unmarshal(body, &shape); err != nil {
		return fmt.Errorf("%w: unreadable collection description", ErrUnavailable)
	}

	size := shape.Result.Config.Params.Vectors.Size
	if size != dimensions {
		return fmt.Errorf(
			"%w: collection holds %d-dimensional vectors but the embedding model is configured for %d",
			ErrUnavailable, size, dimensions,
		)
	}

	if distance := shape.Result.Config.Params.Vectors.Distance; distance != "Cosine" {
		return fmt.Errorf("%w: collection distance is %q, want Cosine", ErrUnavailable, distance)
	}

	return nil
}

// UpsertPoints writes vectors, replacing any point with the same id.
//
// The wait flag makes the write observable before it returns, so a chunk marked
// ready can never refer to a vector that is still in flight.
func (q *QdrantClient) UpsertPoints(ctx context.Context, points []VectorPoint) error {
	if len(points) == 0 {
		return nil
	}

	encoded := make([]map[string]any, 0, len(points))
	for _, point := range points {
		encoded = append(encoded, map[string]any{
			"id":     point.ID,
			"vector": point.Vector,
			"payload": map[string]string{
				payloadClassID:          strconv.FormatUint(point.ClassID, 10),
				payloadMaterialID:       strconv.FormatUint(point.MaterialID, 10),
				payloadKnowledgeEntryID: strconv.FormatUint(point.KnowledgeEntryID, 10),
				payloadChunkID:          strconv.FormatUint(point.ID, 10),
				payloadChunkIndex:       strconv.Itoa(point.ChunkIndex),
			},
		})
	}

	status, _, err := q.do(ctx, http.MethodPut, "/collections/"+q.collection+"/points?wait=true",
		map[string]any{"points": encoded})
	if err != nil {
		return err
	}

	if status != http.StatusOK {
		return fmt.Errorf("%w: vector write answered %d", ErrUnavailable, status)
	}

	return nil
}

// Query returns the nearest vectors for this class above the score threshold.
//
// The class filter is sent to the store as well as re-checked in MySQL: the
// store's answer decides which candidates to consider, never whether the caller
// is allowed to see them.
func (q *QdrantClient) Query(
	ctx context.Context,
	vector []float32,
	classID uint64,
	limit int,
	threshold float64,
) ([]ScoredPoint, error) {
	request := map[string]any{
		"query":           vector,
		"limit":           limit,
		"with_payload":    false,
		"score_threshold": threshold,
		"filter": map[string]any{
			"must": []map[string]any{{
				"key":   payloadClassID,
				"match": map[string]string{"value": strconv.FormatUint(classID, 10)},
			}},
		},
	}

	status, body, err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/query", request)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: vector query answered %d", ErrUnavailable, status)
	}

	return parseScoredPoints(body)
}

// queryResponse is the shape the query endpoint answers with.
type queryResponse struct {
	Result struct {
		Points []struct {
			ID    json.Number `json:"id"`
			Score float64     `json:"score"`
		} `json:"points"`
	} `json:"result"`
}

func parseScoredPoints(body []byte) ([]ScoredPoint, error) {
	var response queryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: unreadable vector response", ErrUnavailable)
	}

	points := make([]ScoredPoint, 0, len(response.Result.Points))

	for _, point := range response.Result.Points {
		id, err := strconv.ParseUint(point.ID.String(), 10, 64)
		if err != nil {
			// A point this API cannot identify cannot be resolved back to a
			// chunk, so it is dropped rather than guessed at.
			continue
		}

		points = append(points, ScoredPoint{ID: id, Score: point.Score})
	}

	return points, nil
}

// DeletePoints removes vectors by id. Deleting an id that is already gone is
// not an error, which keeps the compensation path idempotent.
func (q *QdrantClient) DeletePoints(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}

	status, _, err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/delete?wait=true",
		map[string]any{"points": ids})
	if err != nil {
		return err
	}

	if status != http.StatusOK {
		return fmt.Errorf("%w: vector delete answered %d", ErrUnavailable, status)
	}

	return nil
}

// DeleteForMaterial removes every vector belonging to one material, whatever
// generation wrote it.
func (q *QdrantClient) DeleteForMaterial(ctx context.Context, classID, materialID uint64) error {
	request := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": payloadClassID, "match": map[string]string{"value": strconv.FormatUint(classID, 10)}},
				{"key": payloadMaterialID, "match": map[string]string{"value": strconv.FormatUint(materialID, 10)}},
			},
		},
	}

	status, _, err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/delete?wait=true", request)
	if err != nil {
		return err
	}

	if status != http.StatusOK {
		return fmt.Errorf("%w: vector delete answered %d", ErrUnavailable, status)
	}

	return nil
}

// do sends one request and returns the status and body.
//
// Every transport failure and every non-success status becomes ErrUnavailable
// carrying only the status code: the store's address, its body and any key stay
// on the server side.
func (q *QdrantClient) do(
	ctx context.Context,
	method string,
	path string,
	body any,
) (int, []byte, error) {
	var payload io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode vector request: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	// A base URL may carry a path prefix of its own, so the route is appended
	// to it rather than replacing it. The query string is split off first:
	// assigning it as part of Path would percent-encode the "?" and turn the
	// route into an unmatched path.
	endpoint := *q.base

	route := path
	if index := strings.IndexByte(path, '?'); index >= 0 {
		route = path[:index]
		endpoint.RawQuery = path[index+1:]
	}

	endpoint.Path = strings.TrimSuffix(q.base.Path, "/") + route

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), payload)
	if err != nil {
		return 0, nil, fmt.Errorf("build vector request: %w", err)
	}

	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := q.http.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: vector store is unreachable", ErrUnavailable)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: vector store response could not be read", ErrUnavailable)
	}

	return response.StatusCode, responseBody, nil
}
