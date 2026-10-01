package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"campusclaw/internal/chunking"
)

// Index statuses, matching the enum in the migration.
const (
	statusPending = "pending"
	statusReady   = "ready"
	statusFailed  = "failed"
)

// separatorColumn is the index table's separator column, quoted because
// separator is a reserved word in MySQL and an unquoted reference is a syntax
// error.
const separatorColumn = "`separator`"

// lockWaitSeconds bounds how long a second rebuild of the same material waits
// for the first. An unbounded wait would let one stuck index block a worker
// forever.
const lockWaitSeconds = 5

// Store reads and writes the chunk index in MySQL.
type Store struct {
	db *sql.DB
}

// NewStore builds a store over an existing pool.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Source is a material's indexable text and the identifiers indexing needs.
type Source struct {
	ClassID          uint64
	MaterialID       uint64
	KnowledgeEntryID uint64
	Title            string
	Body             string
}

// TextChunk is a chunk about to be written.
type TextChunk struct {
	ChunkIndex  int
	Text        string
	StartOffset int
	EndOffset   int
}

// IndexState is one material's index bookkeeping.
type IndexState struct {
	Generation       int
	Status           string
	Strategy         chunking.Strategy
	MaxChars         int
	OverlapPercent   int
	Separator        chunking.Separator
	RemoveURLsEmails bool
	FoldWhitespace   bool
	FailureCode      string
	UpdatedAt        time.Time
}

// Hit is a chunk resolved from MySQL, with the material title joined in.
//
// The excerpt is Text, taken from this row and never from the vector store:
// MySQL is the only authority for what a source says.
type Hit struct {
	ChunkID          uint64
	MaterialID       uint64
	KnowledgeEntryID uint64
	ChunkIndex       int
	Text             string
	StartOffset      int
	EndOffset        int
	OffsetBasis      string
	Title            string
}

// Source returns a material's body text and identifiers, scoped by class.
func (s *Store) Source(ctx context.Context, classID, materialID uint64) (Source, error) {
	var source Source

	err := s.db.QueryRowContext(ctx, `
		SELECT k.class_id, k.material_id, k.id, m.title, k.body_text
		  FROM knowledge_entries k
		  JOIN materials m ON m.id = k.material_id AND m.class_id = k.class_id
		 WHERE k.material_id = ? AND k.class_id = ?`,
		materialID, classID,
	).Scan(
		&source.ClassID, &source.MaterialID, &source.KnowledgeEntryID, &source.Title, &source.Body,
	)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Source{}, ErrNotFound
	case err != nil:
		return Source{}, fmt.Errorf("read indexable material: %w", err)
	}

	return source, nil
}

// IndexState returns the material's current index row.
func (s *Store) IndexState(ctx context.Context, classID, entryID uint64) (IndexState, error) {
	var (
		state     IndexState
		strategy  string
		separator string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT generation, status, strategy, max_chars, overlap_percent, `+separatorColumn+`,
		       remove_urls_emails, fold_whitespace, failure_code, updated_at
		  FROM knowledge_indexes
		 WHERE knowledge_entry_id = ? AND class_id = ?`,
		entryID, classID,
	).Scan(
		&state.Generation, &state.Status, &strategy, &state.MaxChars, &state.OverlapPercent,
		&separator, &state.RemoveURLsEmails, &state.FoldWhitespace, &state.FailureCode, &state.UpdatedAt,
	)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return IndexState{}, ErrNotFound
	case err != nil:
		return IndexState{}, fmt.Errorf("read index state: %w", err)
	}

	state.Strategy = chunking.Strategy(strategy)
	state.Separator = chunking.Separator(separator)

	return state, nil
}

// BeginGeneration opens a new index generation for a material.
//
// Writing the new generation is what makes the previous chunks unreachable:
// every query joins on the generation recorded here, so the old slices stop
// being candidates the moment this row changes, before any of them is deleted.
func (s *Store) BeginGeneration(
	ctx context.Context,
	source Source,
	options chunking.Options,
	generation int,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge_indexes
			(knowledge_entry_id, class_id, material_id, generation, status, strategy,
			 max_chars, overlap_percent, `+separatorColumn+`, remove_urls_emails, fold_whitespace,
			 failure_code, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', UTC_TIMESTAMP(6), UTC_TIMESTAMP(6))
		ON DUPLICATE KEY UPDATE
			generation = VALUES(generation),
			status = VALUES(status),
			strategy = VALUES(strategy),
			max_chars = VALUES(max_chars),
			overlap_percent = VALUES(overlap_percent),
			`+separatorColumn+` = VALUES(`+separatorColumn+`),
			remove_urls_emails = VALUES(remove_urls_emails),
			fold_whitespace = VALUES(fold_whitespace),
			failure_code = '',
			updated_at = UTC_TIMESTAMP(6)`,
		source.KnowledgeEntryID, source.ClassID, source.MaterialID, generation, statusPending,
		string(options.Strategy), options.MaxChars, options.OverlapPercent, string(options.Separator),
		options.RemoveURLsEmails, options.FoldWhitespace,
	)
	if err != nil {
		return fmt.Errorf("open index generation: %w", err)
	}

	return nil
}

// ReplaceChunks removes every chunk of an older generation and writes the new
// ones as pending, returning their ids in order.
//
// It runs in one transaction so a reader never sees the old slices deleted
// while the new ones are only half written.
func (s *Store) ReplaceChunks(
	ctx context.Context,
	source Source,
	generation int,
	basis chunking.OffsetBasis,
	chunks []TextChunk,
) ([]uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin chunk transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM knowledge_chunks WHERE knowledge_entry_id = ? AND index_generation <> ?`,
		source.KnowledgeEntryID, generation,
	); err != nil {
		return nil, fmt.Errorf("drop superseded chunks: %w", err)
	}

	ids := make([]uint64, 0, len(chunks))

	for _, chunk := range chunks {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_chunks
				(class_id, material_id, knowledge_entry_id, index_generation, chunk_index,
				 chunk_text, start_offset, end_offset, offset_basis, index_status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			source.ClassID, source.MaterialID, source.KnowledgeEntryID, generation, chunk.ChunkIndex,
			chunk.Text, chunk.StartOffset, chunk.EndOffset, string(basis), statusPending,
		)
		if err != nil {
			return nil, fmt.Errorf("insert chunk %d: %w", chunk.ChunkIndex, err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("read chunk id: %w", err)
		}

		ids = append(ids, uint64(id))
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit chunks: %w", err)
	}

	return ids, nil
}

// SetChunksStatus marks a set of chunks ready or failed.
func (s *Store) SetChunksStatus(ctx context.Context, ids []uint64, status string) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	// The status binds to the SET clause, which comes before the IN list.
	arguments := make([]any, 0, len(ids)+1)
	arguments = append(arguments, status)
	for _, id := range ids {
		arguments = append(arguments, id)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_chunks SET index_status = ? WHERE id IN (`+placeholders+`)`,
		arguments...,
	); err != nil {
		return fmt.Errorf("set chunk status: %w", err)
	}

	return nil
}

// MarkChunksStatus updates every chunk of one generation.
func (s *Store) MarkChunksStatus(ctx context.Context, entryID uint64, generation int, status string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_chunks SET index_status = ?
		  WHERE knowledge_entry_id = ? AND index_generation = ?`,
		status, entryID, generation,
	); err != nil {
		return fmt.Errorf("mark generation chunks: %w", err)
	}

	return nil
}

// SetIndexStatus records the outcome of a generation.
//
// The status is only written when the generation still matches, so a slow
// failure cannot overwrite the outcome of a newer rebuild that already
// superseded it.
func (s *Store) SetIndexStatus(
	ctx context.Context,
	entryID uint64,
	generation int,
	status, failureCode string,
) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE knowledge_indexes
		   SET status = ?, failure_code = ?, updated_at = UTC_TIMESTAMP(6)
		 WHERE knowledge_entry_id = ? AND generation = ?`,
		status, failureCode, entryID, generation,
	); err != nil {
		return fmt.Errorf("set index status: %w", err)
	}

	return nil
}

// ChunkIDsOfGeneration lists the chunk ids a generation wrote, so a failure can
// remove exactly the vectors it may have left behind.
func (s *Store) ChunkIDsOfGeneration(ctx context.Context, entryID uint64, generation int) ([]uint64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM knowledge_chunks WHERE knowledge_entry_id = ? AND index_generation = ?`,
		entryID, generation,
	)
	if err != nil {
		return nil, fmt.Errorf("list generation chunks: %w", err)
	}
	defer rows.Close()

	var ids []uint64
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan chunk id: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// chunkColumns is the projection every chunk read shares.
const chunkColumns = `
	c.id, c.material_id, c.knowledge_entry_id, c.chunk_index, c.chunk_text,
	c.start_offset, c.end_offset, c.offset_basis, m.title`

// currentGenerationJoin restricts a read to chunks of the generation the index
// currently points at, and only while that generation is ready.
const currentGenerationJoin = `
	JOIN knowledge_indexes i
	  ON i.knowledge_entry_id = c.knowledge_entry_id
	 AND i.generation = c.index_generation
	 AND i.status = 'ready'
	JOIN materials m
	  ON m.id = c.material_id
	 AND m.class_id = c.class_id`

func scanHits(rows *sql.Rows) ([]Hit, error) {
	hits := make([]Hit, 0)

	for rows.Next() {
		var hit Hit
		if err := rows.Scan(
			&hit.ChunkID, &hit.MaterialID, &hit.KnowledgeEntryID, &hit.ChunkIndex, &hit.Text,
			&hit.StartOffset, &hit.EndOffset, &hit.OffsetBasis, &hit.Title,
		); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		hits = append(hits, hit)
	}

	return hits, rows.Err()
}

// KeywordRanked is one keyword result with its relevance.
type KeywordRanked struct {
	Hit   Hit
	Score float64
}

// RankedKeywordHits returns the class's ready chunks that match the query, best
// relevance first.
//
// The match is a filter as well as a ranking: a row whose relevance is zero is
// not a weak hit to be padded out, it is not a hit at all.
func (s *Store) RankedKeywordHits(ctx context.Context, classID uint64, query string, limit int) ([]KeywordRanked, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+chunkColumns+`,
		       MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS score
		  FROM knowledge_chunks c`+currentGenerationJoin+`
		 WHERE c.class_id = ?
		   AND c.index_status = 'ready'
		   AND MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE)
		 ORDER BY score DESC, c.id ASC
		 LIMIT ?`,
		query, classID, query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()

	ranked := make([]KeywordRanked, 0)

	for rows.Next() {
		var (
			hit   Hit
			score float64
		)
		if err := rows.Scan(
			&hit.ChunkID, &hit.MaterialID, &hit.KnowledgeEntryID, &hit.ChunkIndex, &hit.Text,
			&hit.StartOffset, &hit.EndOffset, &hit.OffsetBasis, &hit.Title, &score,
		); err != nil {
			return nil, fmt.Errorf("scan keyword hit: %w", err)
		}

		ranked = append(ranked, KeywordRanked{Hit: hit, Score: score})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}

	return ranked, nil
}

// HitsByID resolves chunk ids to their rows, keeping only chunks that belong to
// this class, are ready, and are part of the generation the index currently
// points at.
//
// This is the second guard: the vector store's payload is a hint about which
// candidates to look up, and the class condition here is what decides whether
// the caller may see them. A candidate that fails it is dropped silently.
func (s *Store) HitsByID(ctx context.Context, classID uint64, ids []uint64) ([]Hit, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	arguments := make([]any, 0, len(ids)+1)
	arguments = append(arguments, classID)
	for _, id := range ids {
		arguments = append(arguments, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+chunkColumns+`
		  FROM knowledge_chunks c`+currentGenerationJoin+`
		 WHERE c.class_id = ?
		   AND c.index_status = 'ready'
		   AND c.id IN (`+placeholders+`)`,
		arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve chunk ids: %w", err)
	}
	defer rows.Close()

	return scanHits(rows)
}

// Unindexed returns materials whose index is missing, pending or failed, in
// insertion order.
//
// Pending and failed are included so a restart finishes work an earlier run
// abandoned, and because a failed index is the state a teacher retries from.
func (s *Store) Unindexed(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.class_id, k.material_id, k.id, m.title, k.body_text
		  FROM knowledge_entries k
		  JOIN materials m ON m.id = k.material_id AND m.class_id = k.class_id
		  LEFT JOIN knowledge_indexes i ON i.knowledge_entry_id = k.id
		 WHERE i.knowledge_entry_id IS NULL
		    OR i.status <> 'ready'
		 ORDER BY k.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list unindexed materials: %w", err)
	}
	defer rows.Close()

	sources := make([]Source, 0)

	for rows.Next() {
		var source Source
		if err := rows.Scan(
			&source.ClassID, &source.MaterialID, &source.KnowledgeEntryID, &source.Title, &source.Body,
		); err != nil {
			return nil, fmt.Errorf("scan unindexed material: %w", err)
		}
		sources = append(sources, source)
	}

	return sources, rows.Err()
}

// WithMaterialLock runs fn while holding an advisory lock for one material, so
// two rebuilds of the same material cannot interleave and leave a mixture of
// two strategies behind.
//
// The lock is a MySQL named lock held on one dedicated connection for the whole
// operation, and it is released even when fn fails.
func (s *Store) WithMaterialLock(
	ctx context.Context,
	classID, materialID uint64,
	fn func(context.Context) error,
) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire lock connection: %w", err)
	}
	defer conn.Close()

	name := fmt.Sprintf("campusclaw_index_%d_%d", classID, materialID)

	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, lockWaitSeconds).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire index lock: %w", err)
	}

	if !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("%w: another rebuild of this material is in progress", ErrUnavailable)
	}

	defer func() {
		// The lock dies with the connection anyway; releasing explicitly keeps
		// the connection usable for the rest of its pooled life.
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", name)
	}()

	return fn(ctx)
}
