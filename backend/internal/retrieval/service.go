package retrieval

import (
	"context"
	"database/sql"
	"time"

	"campusclaw/internal/chunking"
	"campusclaw/internal/config"
)

// Timeouts for the retrieval dependencies. A vector lookup is a local call to
// an internal service; a model gateway answers over the network and gets a
// longer budget.
const (
	qdrantTimeout  = 10 * time.Second
	gatewayTimeout = 30 * time.Second
)

// NewFromConfig builds the service from the server configuration.
func NewFromConfig(cfg *config.Config, db *sql.DB) *Service {
	store := NewStore(db)
	vectors := NewQdrant(cfg.Qdrant.URL, cfg.Qdrant.Collection, qdrantTimeout)
	embedder := NewEmbedder(
		cfg.Embedding.BaseURL, cfg.Embedding.Model, cfg.Embedding.APIKey,
		cfg.Embedding.Dimensions, gatewayTimeout,
	)
	chatter := NewChatter(cfg.Chat.BaseURL, cfg.Chat.Model, cfg.Chat.APIKey, gatewayTimeout)

	return NewService(store, vectors, embedder, chatter, NewIndexer(store, vectors, embedder))
}

// Service answers retrieval requests.
//
// It owns the index as well as the queries over it, so the two cannot drift:
// the same code that writes a generation is the only code that knows how a
// generation is read back.
type Service struct {
	store    *Store
	vectors  *QdrantClient
	embedder *Embedder
	chatter  *Chatter
	indexer  *Indexer
}

// NewService wires the retrieval dependencies.
func NewService(
	store *Store,
	vectors *QdrantClient,
	embedder *Embedder,
	chatter *Chatter,
	indexer *Indexer,
) *Service {
	return &Service{
		store:    store,
		vectors:  vectors,
		embedder: embedder,
		chatter:  chatter,
		indexer:  indexer,
	}
}

// PrepareCollection makes the vector collection match the configured embedding
// width. It runs once at startup; a mismatch is reported rather than corrected,
// because recreating a collection would discard every existing vector.
func (s *Service) PrepareCollection(ctx context.Context) error {
	return s.vectors.EnsureCollection(ctx, s.embedder.Dimensions())
}

// Indexer exposes the index lifecycle for the upload path and for startup
// backfill.
func (s *Service) Indexer() *Indexer { return s.indexer }

// IndexState returns a material's index state for the teacher-facing view.
func (s *Service) IndexState(ctx context.Context, classID, materialID uint64) (IndexState, error) {
	return s.indexer.State(ctx, classID, materialID)
}

// Reindex rebuilds one material's index with an explicitly chosen strategy.
func (s *Service) Reindex(
	ctx context.Context,
	classID, materialID uint64,
	options chunking.Options,
) (IndexState, error) {
	if err := s.indexer.Reindex(ctx, classID, materialID, options); err != nil {
		return IndexState{}, err
	}

	return s.indexer.State(ctx, classID, materialID)
}
