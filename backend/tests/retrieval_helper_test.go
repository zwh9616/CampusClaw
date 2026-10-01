package tests

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// embeddingDimensions is the width the fake gateways and the fake vector store
// agree on. It is small on purpose: a test should be able to read a vector.
const embeddingDimensions = 4

// conceptGroups are the axes the fake embedder recognises.
//
// A text is embedded as a bag of concepts, so a question and a passage that
// share a concept land on the same axis and score alike. That is what lets a
// test exercise "a paraphrase is found by the vector path but not by the
// keyword path" without a real model.
var conceptGroups = [][]string{
	{"向量", "语义", "embedding", "nearest"},
	{"天气", "气温", "降雨"},
	{"考试", "测验", "成绩"},
	{"第一章", "绪论"},
}

// fakeEmbedder is an OpenAI-compatible embedding endpoint.
type fakeEmbedder struct {
	server *httptest.Server

	mu    sync.Mutex
	calls int
	fail  bool
}

func newFakeEmbedder(t *testing.T) *fakeEmbedder {
	t.Helper()

	fake := &fakeEmbedder{}

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}

		fake.mu.Lock()
		fake.calls++
		shouldFail := fake.fail
		fake.mu.Unlock()

		if shouldFail {
			http.Error(w, "upstream is down", http.StatusBadGateway)
			return
		}

		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}

		data := make([]item, 0, len(request.Input))
		for index, text := range request.Input {
			data = append(data, item{Index: index, Embedding: embed(text)})
		}

		writeJSON(t, w, map[string]any{"data": data})
	}))

	t.Cleanup(fake.server.Close)

	return fake
}

func (f *fakeEmbedder) setFailure(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// embed turns text into a bag of concepts.
func embed(text string) []float32 {
	vector := make([]float32, embeddingDimensions)

	for axis, group := range conceptGroups {
		if axis >= embeddingDimensions {
			break
		}
		for _, term := range group {
			if strings.Contains(text, term) {
				vector[axis] = 1
				break
			}
		}
	}

	// A text with no known concept still needs a vector; the last axis is a
	// catch-all so two unrelated texts are not accidentally identical.
	if allZero(vector) {
		vector[len(vector)-1] = 0.01
	}

	return vector
}

func allZero(vector []float32) bool {
	for _, value := range vector {
		if value != 0 {
			return false
		}
	}
	return true
}

// fakeChat is an OpenAI-compatible chat endpoint.
type fakeChat struct {
	server *httptest.Server

	mu       sync.Mutex
	calls    int
	requests []string
	reply    string
	fail     bool
}

func newFakeChat(t *testing.T) *fakeChat {
	t.Helper()

	fake := &fakeChat{reply: "这是一段简短回答。[1]"}

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		joined := ""
		for _, message := range request.Messages {
			joined += message.Role + ":" + message.Content + "\n"
		}

		fake.mu.Lock()
		fake.calls++
		fake.requests = append(fake.requests, joined)
		reply := fake.reply
		shouldFail := fake.fail
		fake.mu.Unlock()

		if shouldFail {
			http.Error(w, "upstream is down", http.StatusBadGateway)
			return
		}

		writeJSON(t, w, map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}},
			},
		})
	}))

	t.Cleanup(fake.server.Close)

	return fake
}

func (f *fakeChat) setReply(reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply = reply
}

func (f *fakeChat) setFailure(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
}

func (f *fakeChat) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// lastPrompt returns the text of the most recent chat request, so a test can
// assert what the model was and was not given.
func (f *fakeChat) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.requests) == 0 {
		return ""
	}
	return f.requests[len(f.requests)-1]
}

// fakeQdrant is a tiny stand-in for the vector store, implementing the routes
// the client uses and ranking candidates by cosine similarity.
type fakeQdrant struct {
	server *httptest.Server

	mu         sync.Mutex
	points     map[uint64]storedPoint
	failQuery  bool
	failUpsert bool
	upserted   [][]string
}

type storedPoint struct {
	vector  []float32
	payload map[string]string
}

func newFakeQdrant(t *testing.T) *fakeQdrant {
	t.Helper()

	fake := &fakeQdrant{points: map[uint64]storedPoint{}}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /collections/campusclaw_chunks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"result": map[string]any{
				"config": map[string]any{
					"params": map[string]any{
						"vectors": map[string]any{"size": embeddingDimensions, "distance": "Cosine"},
					},
				},
			},
		})
	})

	mux.HandleFunc("PUT /collections/campusclaw_chunks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"result": true})
	})

	mux.HandleFunc("PUT /collections/campusclaw_chunks/points", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		shouldFail := fake.failUpsert
		fake.mu.Unlock()

		if shouldFail {
			http.Error(w, "write refused", http.StatusServiceUnavailable)
			return
		}

		var request struct {
			Points []struct {
				ID      uint64            `json:"id"`
				Vector  []float32         `json:"vector"`
				Payload map[string]string `json:"payload"`
			} `json:"points"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		fake.mu.Lock()
		for _, point := range request.Points {
			fake.points[point.ID] = storedPoint{vector: point.Vector, payload: point.Payload}
		}
		for _, point := range request.Points {
			keys := make([]string, 0, len(point.Payload))
			for key := range point.Payload {
				keys = append(keys, key)
			}
			fake.upserted = append(fake.upserted, keys)
		}
		fake.mu.Unlock()

		writeJSON(t, w, map[string]any{"result": map[string]any{"operation_id": 1, "status": "completed"}})
	})

	mux.HandleFunc("POST /collections/campusclaw_chunks/points/query", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		shouldFail := fake.failQuery
		fake.mu.Unlock()

		if shouldFail {
			http.Error(w, "query refused", http.StatusServiceUnavailable)
			return
		}

		var request struct {
			Query          []float32 `json:"query"`
			Limit          int       `json:"limit"`
			ScoreThreshold *float64  `json:"score_threshold"`
			Filter         struct {
				Must []struct {
					Key   string `json:"key"`
					Match struct {
						Value string `json:"value"`
					} `json:"match"`
				} `json:"must"`
			} `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		classID := ""
		for _, clause := range request.Filter.Must {
			if clause.Key == "class_id" {
				classID = clause.Match.Value
			}
		}

		threshold := 0.0
		if request.ScoreThreshold != nil {
			threshold = *request.ScoreThreshold
		}

		fake.mu.Lock()
		matches := make([]scoredPointJSON, 0, len(fake.points))
		for id, point := range fake.points {
			if classID != "" && point.payload["class_id"] != classID {
				continue
			}
			score := cosine(request.Query, point.vector)
			if score < threshold {
				continue
			}
			matches = append(matches, scoredPointJSON{ID: id, Score: score})
		}
		fake.mu.Unlock()

		// Deterministic order: score first, then id, so a tie is reproducible.
		sortScored(matches)

		if request.Limit > 0 && len(matches) > request.Limit {
			matches = matches[:request.Limit]
		}

		writeJSON(t, w, map[string]any{"result": map[string]any{"points": matches}})
	})

	mux.HandleFunc("POST /collections/campusclaw_chunks/points/delete", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Points []uint64 `json:"points"`
			Filter *struct {
				Must []struct {
					Key   string `json:"key"`
					Match struct {
						Value string `json:"value"`
					} `json:"match"`
				} `json:"must"`
			} `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		fake.mu.Lock()
		defer fake.mu.Unlock()

		for _, id := range request.Points {
			delete(fake.points, id)
		}

		if request.Filter != nil {
			wanted := map[string]string{}
			for _, clause := range request.Filter.Must {
				wanted[clause.Key] = clause.Match.Value
			}

			for id, point := range fake.points {
				matches := true
				for key, value := range wanted {
					if point.payload[key] != value {
						matches = false
						break
					}
				}
				if matches {
					delete(fake.points, id)
				}
			}
		}

		writeJSON(t, w, map[string]any{"result": map[string]any{"operation_id": 2, "status": "completed"}})
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)

	return fake
}

// inject stores a point directly, which is how a test forges a payload that
// claims a class the chunk does not belong to.
func (f *fakeQdrant) inject(id uint64, payload map[string]string, vector []float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.points[id] = storedPoint{vector: vector, payload: payload}
}

func (f *fakeQdrant) pointCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.points)
}

func (f *fakeQdrant) setQueryFailure(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failQuery = fail
}

func (f *fakeQdrant) setUpsertFailure(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failUpsert = fail
}

func (f *fakeQdrant) upsertedPayloadKeys() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	copied := make([][]string, len(f.upserted))
	copy(copied, f.upserted)
	return copied
}

// scoredPointJSON is the shape the query endpoint answers with.
type scoredPointJSON struct {
	ID    uint64  `json:"id"`
	Score float64 `json:"score"`
}

func sortScored(points []scoredPointJSON) {
	for i := 1; i < len(points); i++ {
		for j := i; j > 0; j-- {
			if points[j].Score > points[j-1].Score ||
				(points[j].Score == points[j-1].Score && points[j].ID < points[j-1].ID) {
				points[j], points[j-1] = points[j-1], points[j]
				continue
			}
			break
		}
	}
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for index := range a {
		dot += float64(a[index]) * float64(b[index])
		normA += float64(a[index]) * float64(a[index])
		normB += float64(b[index]) * float64(b[index])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encode fake response: %v", err)
	}
}

// qdrantPayloadKey is the class key the payload filter uses.
const qdrantPayloadKey = "class_id"

// payloadClass renders a class id the way the client writes it into a payload.
func payloadClass(classID uint64) string {
	return strconv.FormatUint(classID, 10)
}
