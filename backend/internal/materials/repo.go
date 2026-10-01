package materials

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"campusclaw/internal/httpx"
)

// ErrNotFound covers every reason a material cannot be served: it does not
// exist, it belongs to another class, or it is not addressable. Callers map all
// of them to the same 404 so the API never discloses whether a row exists.
var ErrNotFound = errors.New("material not found")

// Repo reads and writes teaching material.
//
// Every read method takes the caller's class id explicitly. There is
// intentionally no lookup by primary key alone, so a handler cannot reach
// another class's material even by mistake.
type Repo struct {
	db *sql.DB
}

// NewRepo builds a repository over an existing pool.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

const materialColumns = `id, class_id, uploaded_by, title, original_filename, content_type, created_at`

// List returns the class's material, newest first.
func (r *Repo) List(ctx context.Context, classID httpx.ID) ([]Material, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT `+materialColumns+`
		   FROM materials
		  WHERE class_id = ?
		  ORDER BY created_at DESC, id DESC`,
		uint64(classID),
	)
	if err != nil {
		return nil, fmt.Errorf("list materials: %w", err)
	}
	defer rows.Close()

	materials := make([]Material, 0)

	for rows.Next() {
		var material Material
		if err := rows.Scan(
			&material.ID,
			&material.ClassID,
			&material.UploadedBy,
			&material.Title,
			&material.OriginalFilename,
			&material.ContentType,
			&material.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan material: %w", err)
		}
		materials = append(materials, material)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list materials: %w", err)
	}

	return materials, nil
}

// NewMaterial is the input for a single upload.
type NewMaterial struct {
	ClassID          httpx.ID
	UploadedBy       httpx.ID
	Title            string
	OriginalFilename string
	StoredFilename   string
	ContentType      string
	Content          string
}

// Create writes the material row and its extracted text in one transaction.
//
// Either both rows exist or neither does. A failure anywhere rolls back, and
// the caller stays responsible for removing the already-saved original.
func (r *Repo) Create(ctx context.Context, input NewMaterial) (Material, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Material{}, fmt.Errorf("begin material transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	result, err := tx.ExecContext(ctx, `
		INSERT INTO materials (class_id, uploaded_by, title, original_filename, stored_filename, content_type)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uint64(input.ClassID), uint64(input.UploadedBy), input.Title,
		input.OriginalFilename, input.StoredFilename, input.ContentType,
	)
	if err != nil {
		return Material{}, fmt.Errorf("insert material: %w", err)
	}

	materialID, err := result.LastInsertId()
	if err != nil {
		return Material{}, fmt.Errorf("read new material id: %w", err)
	}

	if knowledgeInsertHook != nil {
		if err := knowledgeInsertHook(); err != nil {
			return Material{}, fmt.Errorf("insert knowledge entry: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO knowledge_entries (class_id, material_id, body_text) VALUES (?, ?, ?)`,
		uint64(input.ClassID), uint64(materialID), input.Content,
	); err != nil {
		return Material{}, fmt.Errorf("insert knowledge entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Material{}, fmt.Errorf("commit material transaction: %w", err)
	}

	if unknownCommit {
		return Material{}, ErrCommitUnknown
	}

	return r.Metadata(ctx, httpx.ID(materialID), input.ClassID)
}

// Detail returns one material together with its extracted text. Both queries
// are scoped by class id, so a cross-class id simply finds nothing.
func (r *Repo) Detail(ctx context.Context, id, classID httpx.ID) (Material, string, error) {
	material, err := r.Metadata(ctx, id, classID)
	if err != nil {
		return Material{}, "", err
	}

	var content string
	err = r.db.QueryRowContext(
		ctx,
		`SELECT body_text FROM knowledge_entries WHERE material_id = ? AND class_id = ?`,
		uint64(id), uint64(classID),
	).Scan(&content)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Material{}, "", ErrNotFound
	case err != nil:
		return Material{}, "", fmt.Errorf("read knowledge entry: %w", err)
	}

	return material, content, nil
}

// StoredMetadata returns one material's metadata together with its stored
// filename, scoped by class id. It is the only way to reach a stored name, and
// callers must already have authorised the request.
func (r *Repo) StoredMetadata(ctx context.Context, id, classID httpx.ID) (StoredMaterial, error) {
	var stored StoredMaterial

	err := r.db.QueryRowContext(
		ctx,
		`SELECT `+materialColumns+`, stored_filename FROM materials WHERE id = ? AND class_id = ?`,
		uint64(id), uint64(classID),
	).Scan(
		&stored.ID,
		&stored.ClassID,
		&stored.UploadedBy,
		&stored.Title,
		&stored.OriginalFilename,
		&stored.ContentType,
		&stored.CreatedAt,
		&stored.StoredFilename,
	)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return StoredMaterial{}, ErrNotFound
	case err != nil:
		return StoredMaterial{}, fmt.Errorf("read stored material: %w", err)
	}

	return stored, nil
}

// Metadata returns one material's metadata, scoped by class id.
func (r *Repo) Metadata(ctx context.Context, id, classID httpx.ID) (Material, error) {
	var material Material

	err := r.db.QueryRowContext(
		ctx,
		`SELECT `+materialColumns+` FROM materials WHERE id = ? AND class_id = ?`,
		uint64(id), uint64(classID),
	).Scan(
		&material.ID,
		&material.ClassID,
		&material.UploadedBy,
		&material.Title,
		&material.OriginalFilename,
		&material.ContentType,
		&material.CreatedAt,
	)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Material{}, ErrNotFound
	case err != nil:
		return Material{}, fmt.Errorf("read material: %w", err)
	}

	return material, nil
}
