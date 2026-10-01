package retrieval

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// maxHistoryMessages bounds how much of a conversation is carried into the
// generation step. History is context for wording, not evidence.
const maxHistoryMessages = 6

// AskRequest is one question and the conversation that led to it.
type AskRequest struct {
	Question string
	History  []ChatMessage
}

// Answer is a short answer with the sources it is allowed to cite.
type Answer struct {
	Answer    string      `json:"answer"`
	Citations []SearchHit `json:"citations"`
}

// Ask answers a question from this class's material only.
//
// Retrieval uses the latest question alone: earlier turns may frame the wording
// of the answer, but they are never evidence and never widen what is searched.
// With no supporting slice the chat gateway is not called at all.
func (s *Service) Ask(ctx context.Context, classID uint64, request AskRequest) (Answer, error) {
	question := strings.TrimSpace(request.Question)
	if question == "" {
		return Answer{}, fmt.Errorf("%w: question must not be blank", ErrInvalidRequest)
	}

	evidence, err := s.hybridSearch(ctx, classID, question, askEvidenceLimit)
	if err != nil {
		return Answer{}, err
	}

	if len(evidence.Hits) == 0 {
		return Answer{Answer: NoEvidenceMessage, Citations: []SearchHit{}}, nil
	}

	system := evidenceInstruction(evidence.Hits)

	messages := make([]ChatMessage, 0, len(request.History)+1)
	messages = append(messages, sanitiseHistory(request.History)...)
	messages = append(messages, ChatMessage{Role: "user", Content: question})

	answer, err := s.chatter.Complete(ctx, system, messages)
	if err != nil {
		return Answer{}, err
	}

	return Answer{
		Answer: answer,
		// The citation list is the evidence the server supplied, numbered in
		// this order. A number the model invented cannot appear here, so an
		// unverifiable marker has nothing to point at.
		Citations: evidence.Hits,
	}, nil
}

// sanitiseHistory keeps the conversation the model may use for wording.
//
// Anything the browser sends as a system message is dropped: the instruction
// that constrains the answer is written by the server, and a client must not be
// able to replace it. The tail is kept, because the most recent turns are the
// ones that shape the current question.
func sanitiseHistory(history []ChatMessage) []ChatMessage {
	kept := make([]ChatMessage, 0, len(history))

	for _, message := range history {
		switch message.Role {
		case "user", "assistant":
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			kept = append(kept, message)
		default:
			// A system turn from the client, or anything else unexpected.
		}
	}

	if len(kept) > maxHistoryMessages {
		kept = kept[len(kept)-maxHistoryMessages:]
	}

	return kept
}

// evidenceInstruction writes the server-side constraint and the numbered
// evidence blocks.
//
// Only this class's titles, slice numbers and text are included: the model is
// asked to write about what it was given, not to go and look for more.
func evidenceInstruction(hits []SearchHit) string {
	var builder strings.Builder

	builder.WriteString("你是课程资料问答助手。只能依据下面提供的资料片段作答，不要使用资料以外的知识，也不要编造来源。\n")
	builder.WriteString("要求：\n")
	builder.WriteString("1. 用不超过三句话简短回答。\n")
	builder.WriteString("2. 每个结论后用 [编号] 标注来源，编号只能是下面的编号。\n")
	fmt.Fprintf(&builder, "3. 资料不足以回答时，只回复「%s」。\n\n", NoEvidenceMessage)
	builder.WriteString("资料：\n")

	for position, hit := range hits {
		fmt.Fprintf(&builder, "[%d] 材料《%s》第 %d 片（字符 %d-%d）：\n%s\n\n",
			position+1, hit.Title, hit.ChunkIndex, hit.StartOffset, hit.EndOffset, hit.Excerpt)
	}

	return strings.TrimSpace(builder.String())
}

// ValidCitations reports which evidence numbers an answer actually refers to.
//
// It exists so a caller can check that every marker in the answer points at a
// slice the server supplied. A marker outside that range is unverifiable and is
// reported as such rather than being presented as a source.
func ValidCitations(answer string, evidence int) []int {
	seen := make(map[int]bool)
	ordered := make([]int, 0)

	for _, number := range citationNumbers(answer) {
		if number < 1 || number > evidence || seen[number] {
			continue
		}
		seen[number] = true
		ordered = append(ordered, number)
	}

	return ordered
}

// citationNumbers extracts every [n] marker in the answer.
func citationNumbers(answer string) []int {
	var numbers []int

	for index := 0; index < len(answer); index++ {
		if answer[index] != '[' {
			continue
		}

		end := strings.IndexByte(answer[index:], ']')
		if end < 0 {
			break
		}

		raw := answer[index+1 : index+end]
		if value, err := strconv.Atoi(raw); err == nil {
			numbers = append(numbers, value)
		}

		index += end
	}

	return numbers
}
