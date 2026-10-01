package retrieval

import (
	"context"
	"errors"
	"log"

	"campusclaw/internal/chunking"
)

// embedBatchSize keeps 4096-dimensional responses below the gateway body cap.
const embedBatchSize = 8

// Indexer builds and rebuilds the chunk index.
//
// The upload that produced a material is already committed before any of this
// runs, so an indexing failure never undoes a successful upload: it records a
// failed index and leaves the original text and file in place.
type Indexer struct {
	store    *Store
	vectors  *QdrantClient
	embedder *Embedder
}

// NewIndexer wires the indexer's dependencies.
func NewIndexer(store *Store, vectors *QdrantClient, embedder *Embedder) *Indexer {
	return &Indexer{store: store, vectors: vectors, embedder: embedder}
}

// IndexMaterial rebuilds one material's index under that material's lock.
func (i *Indexer) IndexMaterial(
	ctx context.Context,
	classID, materialID uint64,
	options chunking.Options,
) error {
	source, err := i.store.Source(ctx, classID, materialID)
	if err != nil {
		return err
	}

	return i.store.WithMaterialLock(ctx, classID, materialID, func(ctx context.Context) error {
		return i.rebuild(ctx, source, options)
	})
}

// Backfill indexes every material that has no usable index, one at a time.
//
// It uses the auto strategy and is safe to run on every start: each run opens a
// fresh generation and drops the previous chunks, so a repeat never accumulates
// duplicate slices. One material's failure does not stop the rest.
func (i *Indexer) Backfill(ctx context.Context) (int, error) {
	sources, err := i.store.Unindexed(ctx)
	if err != nil {
		return 0, err
	}

	auto, err := chunking.ParseRequest(chunking.Request{})
	if err != nil {
		return 0, err
	}

	indexed := 0

	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return indexed, err
		}

		err := i.store.WithMaterialLock(ctx, source.ClassID, source.MaterialID, func(ctx context.Context) error {
			return i.rebuild(ctx, source, auto)
		})
		if err != nil {
			// One material's failure is recorded against that material and
			// reported here; it must not stop the others from being indexed.
			log.Printf("retrieval: backfill of material %d failed: %v", source.MaterialID, err)
			continue
		}

		indexed++
	}

	return indexed, nil
}

// rebuild produces one generation of chunks and vectors.
func (i *Indexer) rebuild(ctx context.Context, source Source, options chunking.Options) error {
	generation, err := i.nextGeneration(ctx, source)
	if err != nil {
		return err
	}

	// Opening the generation retires the previous one: every read joins on the
	// generation recorded here, so the old slices stop being candidates before
	// any of them is removed.
	if err := i.store.BeginGeneration(ctx, source, options, generation); err != nil {
		return err
	}

	// Clearing the material's vectors here means a rebuild cannot leave a
	// vector from an earlier strategy behind, even if the split changes shape.
	if err := i.vectors.DeleteForMaterial(ctx, source.ClassID, source.MaterialID); err != nil {
		return i.fail(ctx, source, generation, "vector_reset_failed", err)
	}

	result, err := chunking.Split(source.Body, options)
	if err != nil {
		return i.fail(ctx, source, generation, "split_failed", err)
	}

	chunks := make([]TextChunk, 0, len(result.Chunks))
	for _, chunk := range result.Chunks {
		chunks = append(chunks, TextChunk{
			ChunkIndex:  chunk.Index,
			Text:        chunk.Text,
			StartOffset: chunk.Start,
			EndOffset:   chunk.End,
		})
	}

	ids, err := i.store.ReplaceChunks(ctx, source, generation, result.Basis, chunks)
	if err != nil {
		return i.fail(ctx, source, generation, "chunk_write_failed", err)
	}

	if len(ids) == 0 {
		// An empty body has nothing to embed, and that is not a failure.
		return i.store.SetIndexStatus(ctx, source.KnowledgeEntryID, generation, statusReady, "")
	}

	if err := i.embedAndStore(ctx, source, generation, ids, chunks); err != nil {
		return err
	}

	if err := i.store.SetChunksStatus(ctx, ids, statusReady); err != nil {
		return err
	}

	return i.store.SetIndexStatus(ctx, source.KnowledgeEntryID, generation, statusReady, "")
}

// embedAndStore writes the vectors for a generation in batches.
//
// A failure at any batch marks the whole generation failed and removes the
// vectors already written, so there is never a partially indexed material whose
// slices are searchable but incomplete.
func (i *Indexer) embedAndStore(
	ctx context.Context,
	source Source,
	generation int,
	ids []uint64,
	chunks []TextChunk,
) error {
	for start := 0; start < len(ids); start += embedBatchSize {
		end := min(start+embedBatchSize, len(ids))

		texts := make([]string, 0, end-start)
		for _, chunk := range chunks[start:end] {
			texts = append(texts, chunk.Text)
		}

		vectors, err := i.embedder.Embed(ctx, texts)
		if err != nil {
			return i.fail(ctx, source, generation, "embedding_failed", err)
		}

		points := make([]VectorPoint, 0, len(vectors))
		for offset, vector := range vectors {
			chunkID := ids[start+offset]
			points = append(points, VectorPoint{
				ID:               chunkID,
				Vector:           vector,
				ClassID:          source.ClassID,
				MaterialID:       source.MaterialID,
				KnowledgeEntryID: source.KnowledgeEntryID,
				ChunkIndex:       chunks[start+offset].ChunkIndex,
			})
		}

		if err := i.vectors.UpsertPoints(ctx, points); err != nil {
			return i.fail(ctx, source, generation, "vector_write_failed", err)
		}
	}

	return nil
}

// nextGeneration returns the generation this rebuild should write.
func (i *Indexer) nextGeneration(ctx context.Context, source Source) (int, error) {
	state, err := i.store.IndexState(ctx, source.ClassID, source.KnowledgeEntryID)

	switch {
	case errors.Is(err, ErrNotFound):
		return 1, nil
	case err != nil:
		return 0, err
	default:
		return state.Generation + 1, nil
	}
}

// fail records a failed generation and removes anything it may have written.
//
// The order matters: the generation is marked failed first, which stops any
// further read from seeing it, and only then are vectors removed. A failure to
// clean up is logged, never returned as a success.
func (i *Indexer) fail(ctx context.Context, source Source, generation int, code string, cause error) error {
	if err := i.store.MarkChunksStatus(ctx, source.KnowledgeEntryID, generation, statusFailed); err != nil {
		log.Printf("retrieval: could not mark chunks of material %d failed: %v", source.MaterialID, err)
	}

	if err := i.store.SetIndexStatus(ctx, source.KnowledgeEntryID, generation, statusFailed, code); err != nil {
		log.Printf("retrieval: could not record failed index for material %d: %v", source.MaterialID, err)
	}

	ids, err := i.store.ChunkIDsOfGeneration(ctx, source.KnowledgeEntryID, generation)
	if err != nil {
		log.Printf("retrieval: could not list chunks of material %d for cleanup: %v", source.MaterialID, err)
		return cause
	}

	if err := i.vectors.DeletePoints(ctx, ids); err != nil {
		log.Printf("retrieval: could not remove partial vectors of material %d: %v", source.MaterialID, err)
	}

	return cause
}

// Reindex is the teacher-facing rebuild: it takes an explicit strategy and
// refuses a material that is not in the caller's class in exactly the same way
// as one that does not exist.
func (i *Indexer) Reindex(
	ctx context.Context,
	classID, materialID uint64,
	options chunking.Options,
) error {
	return i.IndexMaterial(ctx, classID, materialID, options)
}

// StatusNone is the state a material reports when no index generation exists
// yet. It is a presentation value, never stored: an unindexed material is one
// with no knowledge_indexes row at all.
const StatusNone = "none"

// State returns a material's index state, scoped by class so a cross-class id
// is indistinguishable from a missing one.
func (i *Indexer) State(ctx context.Context, classID, materialID uint64) (IndexState, error) {
	source, err := i.store.Source(ctx, classID, materialID)
	if err != nil {
		return IndexState{}, err
	}

	state, err := i.store.IndexState(ctx, classID, source.KnowledgeEntryID)
	if errors.Is(err, ErrNotFound) {
		return IndexState{Status: StatusNone}, nil
	}

	return state, err
}
