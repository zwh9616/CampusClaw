package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The material the retrieval checks work against. The two topics are chosen so
// a passage and a question can share a meaning without sharing a word: the
// keyword path only matches the literal term, the vector path matches the
// concept.
const (
	retrievalTitle   = "向量检索讲义"
	retrievalBody    = "本课讲解向量检索的基本原理与实现方式。\n\n检索结果需要给出可以核对的出处。\n"
	retrievalKeyword = "向量检索"
	retrievalSynonym = "语义相似度"
	retrievalAbsent  = "今天天气怎么样"

	otherClassTitle = "B班专属资料"
	otherClassBody  = "天气与气温的观测方法需要每日记录。\n"
	otherClassTerm  = "天气与气温"
)

// indexWait bounds how long a check waits for a background index task.
const indexWait = 45 * time.Second

type hitJSON struct {
	MaterialID  string `json:"material_id"`
	Title       string `json:"title"`
	ChunkID     string `json:"chunk_id"`
	ChunkIndex  int    `json:"chunk_index"`
	StartOffset int    `json:"start_offset"`
	EndOffset   int    `json:"end_offset"`
	OffsetBasis string `json:"offset_basis"`
	Excerpt     string `json:"excerpt"`
	Source      string `json:"source"`

	KeywordRank *int     `json:"keyword_rank"`
	VectorRank  *int     `json:"vector_rank"`
	RRFScore    *float64 `json:"rrf_score"`
}

type searchJSON struct {
	QueryVectorGenerated bool      `json:"query_vector_generated"`
	Message              string    `json:"message"`
	Hits                 []hitJSON `json:"hits"`
}

type answerJSON struct {
	Answer    string    `json:"answer"`
	Citations []hitJSON `json:"citations"`
}

type indexJSON struct {
	Status     string `json:"status"`
	Generation int    `json:"generation"`
	Strategy   string `json:"strategy"`
}

// checkRetrieval runs every retrieval criterion, returning the Class A material
// it created so the browser suite can be pointed at the same run.
func (r *runner) checkRetrieval(teacher, studentA, teacherB, studentB *browser) string {
	materialID := r.checkUploadAndIndex(teacher, studentA)
	r.checkKeywordAndVectorPaths(teacher, materialID)
	r.checkNoEvidence(teacher)
	r.checkClassIsolationRetrieval(studentA, teacherB, studentB)
	r.checkEvidenceAnswer(teacher)
	r.checkRebuildControls(teacher, studentA, materialID)
	return materialID
}

// KR-01/KR-02/MAT-02: an upload carries a split strategy, becomes searchable,
// and an unusable strategy is refused before anything is stored.
func (r *runner) checkUploadAndIndex(teacher, studentA *browser) string {
	r.begin("KR01", "上传切分策略生效且索引最终就绪，非法策略被拒绝")

	material, status, payload := r.uploadAndDecode(teacher, uploadSpec{
		title:       retrievalTitle,
		filename:    "retrieval.md",
		contentType: "text/markdown",
		content:     []byte(retrievalBody),
		fields:      map[string]string{"chunk_strategy": "auto"},
	})
	if status != http.StatusCreated {
		r.fail("upload with an auto strategy: status %d (%s)", status, trim(payload))
		return ""
	}

	r.check(material.ID != "", "the upload returned no material id")

	state, ok := r.awaitIndex(teacher, material.ID)
	if !ok {
		return material.ID
	}
	r.check(state.Status == "ready", "index status = %q, want ready", state.Status)
	r.check(state.Generation >= 1, "index generation = %d, want at least 1", state.Generation)

	// The baseline is taken after the valid upload, which legitimately added one
	// material; only the rejected uploads must leave the class unchanged.
	before := r.materialIDs(teacher)

	// An unusable strategy must not create a material.
	for _, fields := range []map[string]string{
		{"chunk_strategy": "semantic"},
		{"chunk_strategy": "custom", "chunk_max_chars": "99"},
		{"chunk_strategy": "custom", "chunk_overlap_percent": "51"},
	} {
		_, failed, body := r.uploadAndDecode(teacher, uploadSpec{
			title:       "非法切分策略",
			filename:    "invalid.md",
			contentType: "text/markdown",
			content:     []byte("正文内容"),
			fields:      fields,
		})
		r.check(failed == http.StatusBadRequest,
			"upload with %v: status %d, want 400 (%s)", fields, failed, trim(body))
	}

	r.checkUnchanged(before, teacher, "after rejected uploads")

	return material.ID
}

// KR-03/KR-05: the keyword path finds the term, the vector path finds the
// meaning, and every hit carries a checkable range and a plain-text excerpt.
func (r *runner) checkKeywordAndVectorPaths(teacher *browser, materialID string) {
	if materialID == "" {
		return
	}

	r.begin("KR02", "三种检索方式的本班命中与可核对出处")

	for _, mode := range []string{"keyword", "vector", "hybrid"} {
		status, response, body := r.search(teacher, "/api/search", retrievalKeyword, mode)
		if status != http.StatusOK {
			r.fail("%s search: status %d (%s)", mode, status, trim(body))
			continue
		}

		if len(response.Hits) == 0 {
			r.fail("%s search found nothing for a term the material contains", mode)
			continue
		}

		for _, hit := range response.Hits {
			r.check(hit.MaterialID == materialID,
				"%s: hit material %s, want the uploaded material %s", mode, hit.MaterialID, materialID)
			r.check(hit.Excerpt != "" && hit.Title != "", "%s: hit lacks a title or excerpt", mode)
			r.check(hit.Source == "/api/materials/"+hit.MaterialID,
				"%s: source = %q, want the material endpoint", mode, hit.Source)
			r.check(hit.EndOffset > hit.StartOffset,
				"%s: range [%d,%d) is empty", mode, hit.StartOffset, hit.EndOffset)
			r.check(hit.OffsetBasis == "extracted" || hit.OffsetBasis == "normalized",
				"%s: offset basis = %q", mode, hit.OffsetBasis)
			r.check(!strings.Contains(hit.Excerpt, "\"vector\""),
				"%s: the excerpt looks like a vector component", mode)
		}
	}

	// The paraphrase is only reachable through the embedding: the words differ.
	status, keyword, _ := r.search(teacher, "/api/search", retrievalSynonym, "keyword")
	r.check(status == http.StatusOK, "keyword search for a paraphrase: status %d", status)
	r.check(len(keyword.Hits) == 0,
		"keyword search matched %d chunks for a paraphrase, want none", len(keyword.Hits))

	status, vector, body := r.search(teacher, "/api/search", retrievalSynonym, "vector")
	r.check(status == http.StatusOK, "vector search for a paraphrase: status %d (%s)", status, trim(body))
	r.check(len(vector.Hits) > 0, "vector search found nothing for a passage sharing the concept")
	r.check(vector.QueryVectorGenerated, "query_vector_generated = false for a vector search")

	// A keyword search never needs the embedding gateway at all.
	_, keywordResult, _ := r.search(teacher, "/api/search", retrievalKeyword, "keyword")
	r.check(!keywordResult.QueryVectorGenerated, "query_vector_generated = true for a keyword search")
}

// KR-05: nothing relevant is an empty success with a fixed notice.
func (r *runner) checkNoEvidence(teacher *browser) {
	r.begin("KR03", "无依据查询返回 200 空命中与固定文案")

	for _, mode := range []string{"keyword", "vector", "hybrid"} {
		status, response, body := r.search(teacher, "/api/search", retrievalAbsent, mode)
		if status != http.StatusOK {
			r.fail("%s search for unrelated content: status %d (%s)", mode, status, trim(body))
			continue
		}

		r.check(len(response.Hits) == 0, "%s: %d hits for unrelated content, want none", mode, len(response.Hits))
		r.check(response.Message == "资料中未找到相关内容",
			"%s: message = %q, want the fixed notice", mode, response.Message)
	}
}

// KR-04/KR-07: a forged class id changes nothing, and another class's content
// is absent rather than forbidden.
func (r *runner) checkClassIsolationRetrieval(studentA, teacherB, studentB *browser) {
	r.begin("KR04", "凭据班级隔离：伪造 class_id 不改变检索范围")

	other, status, payload := r.uploadAndDecode(teacherB, uploadSpec{
		title:       otherClassTitle,
		filename:    "other.md",
		contentType: "text/markdown",
		content:     []byte(otherClassBody),
	})
	if status != http.StatusCreated {
		r.fail("Class B upload: status %d (%s)", status, trim(payload))
		return
	}

	if _, ok := r.awaitIndex(teacherB, other.ID); !ok {
		return
	}

	// The query shares its concept with Class B's material, so without the
	// class condition the vector path would find it.
	for _, mode := range []string{"keyword", "vector", "hybrid"} {
		path := "/api/search?class_id=" + other.ID
		request := fmt.Sprintf(`{"query":%s,"mode":%s,"class_id":%s}`,
			jsonString(otherClassTerm), jsonString(mode), jsonString(other.ID))

		response, body, err := studentA.postJSON(path, request)
		if err != nil {
			r.fail("%s search as Class A: %v", mode, err)
			continue
		}

		r.check(response.StatusCode == http.StatusOK,
			"%s: status %d, want 200", mode, response.StatusCode)

		var decoded searchJSON
		if err := json.Unmarshal(body, &decoded); err != nil {
			r.fail("%s: decode response: %v", mode, err)
			continue
		}

		r.check(len(decoded.Hits) == 0,
			"%s: Class A saw %d of Class B's chunks", mode, len(decoded.Hits))
		r.check(!strings.Contains(string(body), otherClassTerm),
			"%s: the response leaked Class B content", mode)
	}

	// Class B's own session can find it, which proves the query was capable of
	// matching at all.
	_, own, body := r.search(studentB, "/api/search", otherClassTerm, "hybrid")
	r.check(len(own.Hits) > 0,
		"Class B could not find its own material (%s)", trim(body))
}

// KR-06: an answer cites the slices the server supplied, and an unsupported
// question is answered without the model.
func (r *runner) checkEvidenceAnswer(teacher *browser) {
	r.begin("KR05", "有据问答的引用与无依据固定回答")

	status, response, body := r.ask(teacher, retrievalKeyword+"的基本原理是什么")
	if status != http.StatusOK {
		r.fail("ask with supporting material: status %d (%s)", status, trim(body))
	} else {
		r.check(response.Answer != "", "the answer was empty")
		r.check(len(response.Citations) > 0, "an answer the material supports came back with no citations")

		for index, citation := range response.Citations {
			r.check(citation.Excerpt != "" && citation.Title != "",
				"citation %d lacks a title or excerpt", index+1)
			r.check(citation.Source == "/api/materials/"+citation.MaterialID,
				"citation %d source = %q", index+1, citation.Source)
		}

		// Every marker in the answer points at a supplied slice.
		for _, marker := range citationMarkers(response.Answer) {
			r.check(marker >= 1 && marker <= len(response.Citations),
				"the answer cites [%d] but only %d sources were supplied", marker, len(response.Citations))
		}
	}

	status, unsupported, body := r.ask(teacher, retrievalAbsent)
	if status != http.StatusOK {
		r.fail("ask without supporting material: status %d (%s)", status, trim(body))
		return
	}
	r.check(unsupported.Answer == "资料中未找到相关内容",
		"answer = %q, want the fixed notice", unsupported.Answer)
	r.check(len(unsupported.Citations) == 0,
		"an unsupported question returned %d citations", len(unsupported.Citations))
}

// KR-02/KR-04: a teacher may rebuild their own material, a student may not, and
// another class's material is answered like a missing one.
func (r *runner) checkRebuildControls(teacher, studentA *browser, materialID string) {
	r.begin("KR06", "教师重建索引、学生被拒绝、跨班与不存在同为 404")

	if materialID == "" {
		return
	}

	before, ok := r.awaitIndex(teacher, materialID)
	if !ok {
		return
	}

	request, err := json.Marshal(map[string]string{"chunk_strategy": "hierarchy"})
	if err != nil {
		r.fail("encode reindex request: %v", err)
		return
	}

	response, body, err := teacher.postJSON("/api/materials/"+materialID+"/reindex", string(request))
	if err != nil {
		r.fail("teacher reindex: %v", err)
		return
	}
	r.check(response.StatusCode == http.StatusOK,
		"teacher reindex: status %d (%s)", response.StatusCode, trim(body))

	after, ok := r.awaitIndex(teacher, materialID)
	if !ok {
		return
	}
	r.check(after.Status == "ready", "status after rebuild = %q, want ready", after.Status)
	r.check(after.Strategy == "hierarchy", "recorded strategy = %q, want hierarchy", after.Strategy)
	r.check(after.Generation > before.Generation,
		"generation did not advance: %d then %d", before.Generation, after.Generation)

	// The rebuilt index is still searchable, and the text is unchanged.
	_, found, _ := r.search(teacher, "/api/search", retrievalKeyword, "hybrid")
	r.check(len(found.Hits) > 0, "the material stopped being searchable after a rebuild")

	_, content, err := r.fetchDetail(teacher, materialID)
	if err != nil {
		r.fail("read material after rebuild: %v", err)
	} else {
		r.check(content == retrievalBody, "the stored text changed during a rebuild")
	}

	response, _, _ = studentA.postJSON("/api/materials/"+materialID+"/reindex", string(request))
	r.check(response.StatusCode == http.StatusForbidden,
		"student reindex: status %d, want 403", response.StatusCode)

	response, _, _ = studentA.get("/api/materials/" + materialID + "/index")
	r.check(response.StatusCode == http.StatusForbidden,
		"student index state: status %d, want 403", response.StatusCode)

	response, _, _ = teacher.postJSON("/api/materials/999999/reindex", string(request))
	r.check(response.StatusCode == http.StatusNotFound,
		"reindex of a missing material: status %d, want 404", response.StatusCode)
}

// ------------------------------------------------------------ retrieval API ---

func (r *runner) search(session *browser, path, query, mode string) (int, searchJSON, []byte) {
	body := fmt.Sprintf(`{"query":%s,"mode":%s}`, jsonString(query), jsonString(mode))

	response, payload, err := session.postJSON(path, body)
	if err != nil {
		r.fail("search request failed: %v", err)
		return 0, searchJSON{}, nil
	}

	var decoded searchJSON
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return response.StatusCode, searchJSON{}, payload
	}

	return response.StatusCode, decoded, payload
}

func (r *runner) ask(session *browser, question string) (int, answerJSON, []byte) {
	body := fmt.Sprintf(`{"question":%s}`, jsonString(question))

	response, payload, err := session.postJSON("/api/ask", body)
	if err != nil {
		r.fail("ask request failed: %v", err)
		return 0, answerJSON{}, nil
	}

	var decoded answerJSON
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return response.StatusCode, answerJSON{}, payload
	}

	return response.StatusCode, decoded, payload
}

func (r *runner) indexState(session *browser, materialID string) (indexJSON, bool) {
	response, payload, err := session.get("/api/materials/" + materialID + "/index")
	if err != nil {
		r.fail("index state request failed: %v", err)
		return indexJSON{}, false
	}
	if response.StatusCode != http.StatusOK {
		r.fail("index state: status %d (%s)", response.StatusCode, trim(payload))
		return indexJSON{}, false
	}

	var body struct {
		Index indexJSON `json:"index"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		r.fail("decode index state: %v", err)
		return indexJSON{}, false
	}

	return body.Index, true
}

// awaitIndex polls until a material's index reaches a terminal state.
//
// Indexing runs after the upload has already been answered, so the run observes
// it rather than assuming it happened.
func (r *runner) awaitIndex(session *browser, materialID string) (indexJSON, bool) {
	deadline := time.Now().Add(indexWait)

	for {
		found, ok := r.indexState(session, materialID)
		if !ok {
			return indexJSON{}, false
		}

		if found.Status == "ready" || found.Status == "failed" {
			return found, true
		}

		if time.Now().After(deadline) {
			r.fail("material %s stayed %q for %s", materialID, found.Status, indexWait)
			return found, false
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// jsonString renders a Go string as a JSON string literal.
func jsonString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// citationMarkers extracts the [n] markers an answer used.
func citationMarkers(answer string) []int {
	var markers []int

	for index := 0; index < len(answer); index++ {
		if answer[index] != '[' {
			continue
		}
		end := strings.IndexByte(answer[index:], ']')
		if end < 0 {
			break
		}

		var number int
		if _, err := fmt.Sscanf(answer[index+1:index+end], "%d", &number); err == nil {
			markers = append(markers, number)
		}

		index += end
	}

	return markers
}
