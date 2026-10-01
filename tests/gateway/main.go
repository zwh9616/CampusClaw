// Command gateway is a deterministic stand-in for the embedding and chat
// gateways, used to exercise the retrieval paths end to end.
//
// It is acceptance tooling, not part of the product: it exists so the
// end-to-end run can be repeated on a machine with no model provider
// configured. Its embeddings are a bag of concepts, so a passage and a question
// that share a concept land on the same dimension and are genuinely similar —
// which is what makes the vector path testable without a model.
//
// Usage:
//
//	EMBEDDING_DIMENSIONS=1536 PORT=8080 go run .
package main

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// concepts are the axes the stub recognises. Each entry is a group of terms
// that mean the same thing for the purpose of this stand-in: a text landing on
// a group's axis is similar to every other text on that axis.
var concepts = [][]string{
	{"向量", "语义", "embedding", "vector", "相似度", "相似"},
	{"检索", "查找", "搜索", "search"},
	{"考试", "测验", "成绩", "exam"},
	{"天气", "气温", "降雨", "weather"},
	{"论文", "文献", "引用"},
	{"矩阵", "线性代数"},
}

// evidenceBlock matches one numbered evidence heading in the system message the
// server writes, e.g. "[2] 材料《讲义》第 3 片（字符 0-10）：".
var evidenceBlock = regexp.MustCompile(`\[(\d+)\] 材料《`)

func main() {
	dimensions := 1536
	if raw := os.Getenv("EMBEDDING_DIMENSIONS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			log.Fatalf("gateway: EMBEDDING_DIMENSIONS must be a positive whole number")
		}
		dimensions = parsed
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", health)
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		handleEmbeddings(w, r, dimensions)
	})
	mux.HandleFunc("POST /v1/chat/completions", handleChat)

	log.Printf("gateway: listening on :%s with %d-dimensional embeddings", port, dimensions)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("gateway: %v", err)
	}
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

type embeddingsRequest struct {
	Input []string `json:"input"`
}

func handleEmbeddings(w http.ResponseWriter, r *http.Request, dimensions int) {
	var request embeddingsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	type item struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}

	data := make([]item, 0, len(request.Input))
	for index, text := range request.Input {
		data = append(data, item{Object: "embedding", Index: index, Embedding: embed(text, dimensions)})
	}

	writeJSON(w, map[string]any{
		"object": "list",
		"data":   data,
		"model":  "stub-embedding",
	})
}

// embed turns text into a unit vector over the concept axes.
//
// A text with no known concept still gets a vector that is orthogonal to every
// concept axis, so it is similar to nothing rather than accidentally similar to
// everything.
func embed(text string, dimensions int) []float32 {
	vector := make([]float32, dimensions)

	for axis, group := range concepts {
		if axis >= dimensions {
			break
		}
		for _, term := range group {
			if strings.Contains(text, term) {
				vector[axis] = 1
				break
			}
		}
	}

	// The last dimension is the "no known concept" axis. It is only used when
	// nothing matched, so unrelated texts cannot collide with a concept.
	used := false
	for index := 0; index < len(concepts) && index < dimensions; index++ {
		if vector[index] != 0 {
			used = true
			break
		}
	}
	if !used {
		vector[dimensions-1] = 1
	}

	normalise(vector)

	return vector
}

func normalise(vector []float32) {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}

	if sum == 0 {
		return
	}

	scale := 1 / math.Sqrt(sum)

	for index := range vector {
		vector[index] = float32(float64(vector[index]) * scale)
	}
}

type chatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// handleChat answers with the citation markers the evidence supports.
//
// It cites only the blocks the server actually supplied, so a run can check
// that the answer's markers and the citation list agree.
func handleChat(w http.ResponseWriter, r *http.Request) {
	var request chatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	system := ""
	for _, message := range request.Messages {
		if message.Role == "system" {
			system = message.Content
		}
	}

	numbers := make([]int, 0)
	seen := map[int]bool{}

	for _, match := range evidenceBlock.FindAllStringSubmatch(system, -1) {
		number, err := strconv.Atoi(match[1])
		if err != nil || seen[number] {
			continue
		}
		seen[number] = true
		numbers = append(numbers, number)
	}

	sort.Ints(numbers)

	answer := "资料中没有可用内容。"
	if len(numbers) > 0 {
		markers := make([]string, 0, len(numbers))
		for _, number := range numbers {
			markers = append(markers, "["+strconv.Itoa(number)+"]")
		}
		answer = "根据本班资料，" + strings.Join(markers, "") + " 给出了相关说明。"
	}

	writeJSON(w, map[string]any{
		"object": "chat.completion",
		"model":  "stub-chat",
		"choices": []map[string]any{
			{"index": 0, "message": map[string]string{"role": "assistant", "content": answer}},
		},
	})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("gateway: encode response: %v", err)
	}
}
