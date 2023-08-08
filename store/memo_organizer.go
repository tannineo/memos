package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type MemoOrganizer struct {
	MemoID int32
	UserID int32
	Pinned bool
}

type FindMemoOrganizer struct {
	MemoID int32
	UserID int32
}

type DeleteMemoOrganizer struct {
	MemoID *int32
	UserID *int32
}

func (s *Store) UpsertMemoOrganizer(ctx context.Context, upsert *MemoOrganizer) (*MemoOrganizer, error) {
	stmt := `
		INSERT INTO memo_organizer (
			memo_id,
			user_id,
			pinned
		)
		VALUES (?, ?, ?)
		ON CONFLICT(memo_id, user_id) DO UPDATE 
		SET
			pinned = EXCLUDED.pinned
	`
	if _, err := s.db.ExecContext(ctx, stmt, upsert.MemoID, upsert.UserID, upsert.Pinned); err != nil {
		return nil, err
	}

	memoOrganizer := upsert
	return memoOrganizer, nil
}

func (s *Store) GetMemoOrganizer(ctx context.Context, find *FindMemoOrganizer) (*MemoOrganizer, error) {
	where, args := []string{}, []any{}
	if find.MemoID != 0 {
		where = append(where, "memo_id = ?")
		args = append(args, find.MemoID)
	}
	if find.UserID != 0 {
		where = append(where, "user_id = ?")
		args = append(args, find.UserID)
	}

	query := fmt.Sprintf(`
		SELECT
			memo_id,
			user_id,
			pinned
		FROM memo_organizer
		WHERE %s
	`, strings.Join(where, " AND "))
	row := s.db.QueryRowContext(ctx, query, args...)
	if err := row.Err(); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}

	memoOrganizer := &MemoOrganizer{}
	if err := row.Scan(
		&memoOrganizer.MemoID,
		&memoOrganizer.UserID,
		&memoOrganizer.Pinned,
	); err != nil {
		return nil, err
	}

	return memoOrganizer, nil
}

func (s *Store) DeleteMemoOrganizer(ctx context.Context, delete *DeleteMemoOrganizer) error {
	where, args := []string{}, []any{}
	if v := delete.MemoID; v != nil {
		where, args = append(where, "memo_id = ?"), append(args, *v)
	}
	if v := delete.UserID; v != nil {
		where, args = append(where, "user_id = ?"), append(args, *v)
	}
	stmt := `DELETE FROM memo_organizer WHERE ` + strings.Join(where, " AND ")
	if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
		return err
	}
	return nil
}

func vacuumMemoOrganizer(ctx context.Context, tx *sql.Tx) error {
	stmt := `
	DELETE FROM 
		memo_organizer 
	WHERE 
		memo_id NOT IN (
			SELECT 
				id 
			FROM 
				memo
		)
		OR user_id NOT IN (
			SELECT 
				id 
			FROM 
				user
		)`
	_, err := tx.ExecContext(ctx, stmt)
	if err != nil {
		return err
	}

	return nil
}
