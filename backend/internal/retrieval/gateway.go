package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxGatewayBody bounds how much of a gateway response is buffered.
const maxGatewayBody = 1 << 20

// Embedder turns text into vectors through an OpenAI-compatible /embeddings
// endpoint.
//
// The key is held here and only ever leaves as an Authorization header. It is
// never logged and never part of an error, so a gateway failure cannot become a
// credential leak.
type Embedder struct {
	base       *url.URL
	model      string
	key        string
	dimensions int
	http       *http.Client
}

// NewEmbedder builds an embedding client.
func NewEmbedder(base *url.URL, model, key string, dimensions int, timeout time.Duration) *Embedder {
	return &Embedder{
		base:       base,
		model:      model,
		key:        key,
		dimensions: dimensions,
		http:       &http.Client{Timeout: timeout},
	}
}

// Dimensions is the width every vector is checked against.
func (e *Embedder) Dimensions() int { return e.dimensions }

// embeddingsRequest is the OpenAI-compatible request body.
type embeddingsRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns one vector per input, in the order the inputs were given.
//
// A response that is short, out of order or the wrong width is a failure, not
// something to pad: indexing half a material would leave the rest silently
// unsearchable.
func (e *Embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	payload, err := json.Marshal(embeddingsRequest{
		Model:          e.model,
		Input:          inputs,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}

	body, err := e.post(ctx, "/embeddings", payload)
	if err != nil {
		return nil, err
	}

	var response embeddingsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: unreadable embedding response", ErrUnavailable)
	}

	if len(response.Data) != len(inputs) {
		return nil, fmt.Errorf("%w: embedding gateway returned %d vectors for %d inputs",
			ErrUnavailable, len(response.Data), len(inputs))
	}

	vectors := make([][]float32, len(inputs))

	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(inputs) {
			return nil, fmt.Errorf("%w: embedding gateway returned an out-of-range index",
				ErrUnavailable)
		}
		if len(item.Embedding) != e.dimensions {
			return nil, fmt.Errorf("%w: embedding width is %d, want %d",
				ErrUnavailable, len(item.Embedding), e.dimensions)
		}
		vectors[item.Index] = item.Embedding
	}

	return vectors, nil
}

// ChatMessage is one turn of the conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chatter writes the short answer through an OpenAI-compatible
// /chat/completions endpoint. It never retrieves anything itself: it is given
// the evidence and asked to write about it.
type Chatter struct {
	base  *url.URL
	model string
	key   string
	http  *http.Client
}

// NewChatter builds a chat client.
func NewChatter(base *url.URL, model, key string, timeout time.Duration) *Chatter {
	return &Chatter{base: base, model: model, key: key, http: &http.Client{Timeout: timeout}}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends the server-written system instruction and the messages, and
// returns the assistant's text.
func (c *Chatter) Complete(ctx context.Context, system string, messages []ChatMessage) (string, error) {
	conversation := make([]ChatMessage, 0, len(messages)+1)
	conversation = append(conversation, ChatMessage{Role: "system", Content: system})
	conversation = append(conversation, messages...)

	payload, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    conversation,
		Temperature: 0,
	})
	if err != nil {
		return "", fmt.Errorf("encode chat request: %w", err)
	}

	body, err := c.post(ctx, "/chat/completions", payload)
	if err != nil {
		return "", err
	}

	var response chatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%w: unreadable chat response", ErrUnavailable)
	}

	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("%w: chat gateway returned no answer", ErrUnavailable)
	}

	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

// post sends a JSON body to one of the gateway's routes.
//
// Every failure becomes ErrUnavailable and carries no response body, status
// text or key: a gateway error is reported as a retryable outage, not as a
// description of the server's internals.
func (c *Chatter) post(ctx context.Context, path string, payload []byte) ([]byte, error) {
	return gatewayPost(ctx, c.http, c.base, path, c.key, payload)
}

func (e *Embedder) post(ctx context.Context, path string, payload []byte) ([]byte, error) {
	return gatewayPost(ctx, e.http, e.base, path, e.key, payload)
}

func gatewayPost(
	ctx context.Context,
	client *http.Client,
	base *url.URL,
	path, key string,
	payload []byte,
) ([]byte, error) {
	endpoint := *base
	endpoint.Path = strings.TrimSuffix(base.Path, "/") + path

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build gateway request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: model gateway is unreachable", ErrUnavailable)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayBody))
	if err != nil {
		return nil, fmt.Errorf("%w: model gateway response could not be read", ErrUnavailable)
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("%w: model gateway answered %d", ErrUnavailable, response.StatusCode)
	}

	return body, nil
}
