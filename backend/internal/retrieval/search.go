package retrieval

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Mode is a retrieval strategy.
type Mode string

const (
	ModeKeyword Mode = "keyword"
	ModeVector  Mode = "vector"
	ModeHybrid  Mode = "hybrid"
)

const (
	// searchLimit is how many hits a search returns.
	searchLimit = 10
	// fusionCandidateLimit is how deep each side of a hybrid search looks
	// before the two rankings are merged.
	fusionCandidateLimit = 20
	// askEvidenceLimit is how many chunks a question is answered from.
	askEvidenceLimit = 4
	// minVectorScore drops weak vector candidates. A cosine similarity below
	// this is not evidence, and padding a result with it would make an
	// unsupported question look answered.
	minVectorScore = 0.35
	// rrfConstant is the k of reciprocal rank fusion. It damps the difference
	// between the top few ranks, which is what lets a chunk that only one side
	// found still surface.
	rrfConstant = 60
	// NoEvidenceMessage is the fixed answer for a question the material cannot
	// support. It is a constant so the API and the page cannot drift apart.
	NoEvidenceMessage = "资料中未找到相关内容"
)

// SearchHit is one result. It carries the material, the slice's position and a
// plain-text excerpt, and it never carries a vector component.
//
// Keyword and vector detail is reported per side with the rank the hit held
// there, or null when it did not enter that side at all.
type SearchHit struct {
	MaterialID  string `json:"material_id"`
	Title       string `json:"title"`
	ChunkID     string `json:"chunk_id"`
	ChunkIndex  int    `json:"chunk_index"`
	StartOffset int    `json:"start_offset"`
	EndOffset   int    `json:"end_offset"`
	OffsetBasis string `json:"offset_basis"`
	Excerpt     string `json:"excerpt"`
	// Source is the authorised material endpoint the excerpt can be opened
	// through. Reaching it still requires the class check.
	Source string `json:"source"`

	KeywordScore *float64 `json:"keyword_score"`
	KeywordRank  *int     `json:"keyword_rank"`
	VectorScore  *float64 `json:"vector_score"`
	VectorRank   *int     `json:"vector_rank"`
	RRFScore     *float64 `json:"rrf_score"`
}

// SearchResult is a complete search response.
type SearchResult struct {
	// QueryVectorGenerated reports whether the embedding gateway was called.
	// A keyword-only search never calls it.
	QueryVectorGenerated bool `json:"query_vector_generated"`
	// Message is the fixed no-evidence notice, present only when nothing was
	// found. An empty result is a result, not an error.
	Message string      `json:"message,omitempty"`
	Hits    []SearchHit `json:"hits"`
}

// ParseMode validates a requested mode, defaulting to hybrid.
func ParseMode(raw string) (Mode, error) {
	switch Mode(strings.TrimSpace(strings.ToLower(raw))) {
	case "":
		return ModeHybrid, nil
	case ModeKeyword:
		return ModeKeyword, nil
	case ModeVector:
		return ModeVector, nil
	case ModeHybrid:
		return ModeHybrid, nil
	default:
		return "", fmt.Errorf("%w: unknown mode", ErrInvalidRequest)
	}
}

// candidate accumulates what each side of the search said about one chunk.
type candidate struct {
	hit Hit

	keywordScore *float64
	keywordRank  *int
	vectorScore  *float64
	vectorRank   *int
	rrf          float64
}

// Search runs one retrieval for the caller's class.
func (s *Service) Search(ctx context.Context, classID uint64, query string, mode Mode) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResult{}, fmt.Errorf("%w: query must not be blank", ErrInvalidRequest)
	}

	switch mode {
	case ModeKeyword:
		return s.keywordSearch(ctx, classID, query, searchLimit)
	case ModeVector:
		return s.vectorSearch(ctx, classID, query, searchLimit)
	case ModeHybrid:
		return s.hybridSearch(ctx, classID, query, searchLimit)
	default:
		return SearchResult{}, fmt.Errorf("%w: unknown mode", ErrInvalidRequest)
	}
}

// keywordSearch answers from the full-text index alone.
func (s *Service) keywordSearch(ctx context.Context, classID uint64, query string, limit int) (SearchResult, error) {
	ranked, err := s.store.RankedKeywordHits(ctx, classID, query, limit)
	if err != nil {
		return SearchResult{}, err
	}

	hits := make([]SearchHit, 0, len(ranked))

	for position, entry := range ranked {
		rank := position + 1
		score := entry.Score

		hits = append(hits, newSearchHit(entry.Hit, func(hit *SearchHit) {
			hit.KeywordScore = &score
			hit.KeywordRank = &rank
		}))
	}

	return SearchResult{Hits: hits, Message: noEvidenceMessage(hits)}, nil
}

// vectorSearch answers from the vector store.
func (s *Service) vectorSearch(ctx context.Context, classID uint64, query string, limit int) (SearchResult, error) {
	ranked, err := s.vectorCandidates(ctx, classID, query, limit)
	if err != nil {
		return SearchResult{}, err
	}

	hits := make([]SearchHit, 0, len(ranked))

	for position, entry := range ranked {
		rank := position + 1
		score := entry.Score

		hits = append(hits, newSearchHit(entry.Hit, func(hit *SearchHit) {
			hit.VectorScore = &score
			hit.VectorRank = &rank
		}))
	}

	return SearchResult{
		Hits:                 hits,
		Message:              noEvidenceMessage(hits),
		QueryVectorGenerated: true,
	}, nil
}

// hybridSearch merges the two sides with reciprocal rank fusion.
//
// Each side is filtered on its own first, then the ranks are combined as
// 1/(k+rank). Raw scores are never added: a full-text relevance and a cosine
// similarity are not on the same scale, and treating them as if they were would
// let whichever had the larger numbers decide the order.
func (s *Service) hybridSearch(ctx context.Context, classID uint64, query string, limit int) (SearchResult, error) {
	keywordRanked, err := s.store.RankedKeywordHits(ctx, classID, query, fusionCandidateLimit)
	if err != nil {
		return SearchResult{}, err
	}

	vectorRanked, err := s.vectorCandidates(ctx, classID, query, fusionCandidateLimit)
	if err != nil {
		return SearchResult{}, err
	}

	candidates := make(map[uint64]*candidate, len(keywordRanked)+len(vectorRanked))

	for position, entry := range keywordRanked {
		rank := position + 1
		score := entry.Score

		item := candidates[entry.Hit.ChunkID]
		if item == nil {
			item = &candidate{hit: entry.Hit}
			candidates[entry.Hit.ChunkID] = item
		}
		item.keywordRank = &rank
		item.keywordScore = &score
		item.rrf += rrf(rank)
	}

	for position, entry := range vectorRanked {
		rank := position + 1
		score := entry.Score

		item := candidates[entry.Hit.ChunkID]
		if item == nil {
			item = &candidate{hit: entry.Hit}
			candidates[entry.Hit.ChunkID] = item
		}
		item.vectorRank = &rank
		item.vectorScore = &score
		item.rrf += rrf(rank)
	}

	ordered := make([]*candidate, 0, len(candidates))
	for _, item := range candidates {
		ordered = append(ordered, item)
	}

	// The fusion score decides the order; the chunk id breaks a tie so the same
	// query always returns the same sequence.
	sort.Slice(ordered, func(a, b int) bool {
		if ordered[a].rrf != ordered[b].rrf {
			return ordered[a].rrf > ordered[b].rrf
		}
		return ordered[a].hit.ChunkID < ordered[b].hit.ChunkID
	})

	if len(ordered) > limit {
		ordered = ordered[:limit]
	}

	hits := make([]SearchHit, 0, len(ordered))

	for _, item := range ordered {
		score := item.rrf
		hits = append(hits, newSearchHit(item.hit, func(hit *SearchHit) {
			hit.KeywordScore = item.keywordScore
			hit.KeywordRank = item.keywordRank
			hit.VectorScore = item.vectorScore
			hit.VectorRank = item.vectorRank
			hit.RRFScore = &score
		}))
	}

	return SearchResult{
		Hits:                 hits,
		Message:              noEvidenceMessage(hits),
		QueryVectorGenerated: true,
	}, nil
}

// ScoredHit is one vector result with its similarity.
type ScoredHit struct {
	Hit   Hit
	Score float64
}

// vectorCandidates embeds the query, asks the store for this class's nearest
// vectors, and resolves them back to rows.
//
// The store's answer only proposes candidates. A point whose chunk cannot be
// read back for this class and generation is dropped, so a payload that lies
// about its class, or a point left over from a retired generation, cannot
// become a hit.
func (s *Service) vectorCandidates(
	ctx context.Context,
	classID uint64,
	query string,
	limit int,
) ([]ScoredHit, error) {
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}

	if len(vectors) != 1 {
		return nil, fmt.Errorf("%w: embedding gateway returned %d vectors for one query",
			ErrUnavailable, len(vectors))
	}

	points, err := s.vectors.Query(ctx, vectors[0], classID, limit, minVectorScore)
	if err != nil {
		return nil, err
	}

	if len(points) == 0 {
		return nil, nil
	}

	ids := make([]uint64, 0, len(points))
	for _, point := range points {
		ids = append(ids, point.ID)
	}

	rows, err := s.store.HitsByID(ctx, classID, ids)
	if err != nil {
		return nil, err
	}

	byID := make(map[uint64]Hit, len(rows))
	for _, row := range rows {
		byID[row.ChunkID] = row
	}

	ranked := make([]ScoredHit, 0, len(points))
	for _, point := range points {
		hit, ok := byID[point.ID]
		if !ok {
			continue
		}
		ranked = append(ranked, ScoredHit{Hit: hit, Score: point.Score})
	}

	return ranked, nil
}

// rrf turns a rank into its fusion contribution.
func rrf(rank int) float64 {
	return 1.0 / float64(rrfConstant+rank)
}

// noEvidenceMessage returns the fixed notice when nothing was found.
func noEvidenceMessage(hits []SearchHit) string {
	if len(hits) == 0 {
		return NoEvidenceMessage
	}
	return ""
}

// newSearchHit renders a stored chunk as a result.
func newSearchHit(hit Hit, decorate func(*SearchHit)) SearchHit {
	result := SearchHit{
		MaterialID:  strconv.FormatUint(hit.MaterialID, 10),
		Title:       hit.Title,
		ChunkID:     strconv.FormatUint(hit.ChunkID, 10),
		ChunkIndex:  hit.ChunkIndex,
		StartOffset: hit.StartOffset,
		EndOffset:   hit.EndOffset,
		OffsetBasis: hit.OffsetBasis,
		Excerpt:     hit.Text,
		Source:      "/api/materials/" + strconv.FormatUint(hit.MaterialID, 10),
	}

	decorate(&result)

	return result
}
