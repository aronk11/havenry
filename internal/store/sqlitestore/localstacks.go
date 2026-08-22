package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aronk11/havenry/internal/store"
)

// localStackMigrations werden an migrations angehängt (siehe sqlite.go).
//
// UNIQUE(host_id, name) statt name allein: derselbe Stack-Name ist auf
// verschiedenen Hosts erlaubt, genau wie bei git-Stacks (ADR-0014).
var localStackMigrations = []string{
	`CREATE TABLE IF NOT EXISTS local_stacks (
		id           TEXT PRIMARY KEY,
		host_id      TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		name         TEXT NOT NULL,
		compose_yaml TEXT NOT NULL,
		created_at   INTEGER NOT NULL,
		updated_at   INTEGER NOT NULL,
		updated_by   TEXT NOT NULL DEFAULT '',
		UNIQUE (host_id, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_local_stacks_host ON local_stacks(host_id)`,
}

func (s *SQLite) CreateLocalStack(ctx context.Context, st store.LocalStack) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO local_stacks (id, host_id, name, compose_yaml, created_at, updated_at, updated_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		st.ID, st.HostID, st.Name, st.ComposeYAML,
		st.CreatedAt.UnixMilli(), st.UpdatedAt.UnixMilli(), st.UpdatedBy)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return store.ErrLocalStackExists
	}
	return err
}

func (s *SQLite) UpdateLocalStack(ctx context.Context, st store.LocalStack) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE local_stacks SET compose_yaml = ?, updated_at = ?, updated_by = ?
		 WHERE host_id = ? AND name = ?`,
		st.ComposeYAML, st.UpdatedAt.UnixMilli(), st.UpdatedBy, st.HostID, st.Name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLite) DeleteLocalStack(ctx context.Context, hostID, name string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM local_stacks WHERE host_id = ? AND name = ?`, hostID, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

const localStackColumns = `id, host_id, name, compose_yaml, created_at, updated_at, updated_by`

func scanLocalStack(row interface{ Scan(...any) error }) (store.LocalStack, error) {
	var st store.LocalStack
	var created, updated int64
	if err := row.Scan(&st.ID, &st.HostID, &st.Name, &st.ComposeYAML,
		&created, &updated, &st.UpdatedBy); err != nil {
		return store.LocalStack{}, err
	}
	st.CreatedAt = time.UnixMilli(created).UTC()
	st.UpdatedAt = time.UnixMilli(updated).UTC()
	return st, nil
}

func (s *SQLite) LocalStackByName(ctx context.Context, hostID, name string) (store.LocalStack, error) {
	st, err := scanLocalStack(s.db.QueryRowContext(ctx,
		`SELECT `+localStackColumns+` FROM local_stacks WHERE host_id = ? AND name = ?`, hostID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return store.LocalStack{}, store.ErrNotFound
	}
	return st, err
}

func (s *SQLite) LocalStacksForHost(ctx context.Context, hostID string) ([]store.LocalStack, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+localStackColumns+` FROM local_stacks WHERE host_id = ? ORDER BY name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectLocalStacks(rows)
}

func (s *SQLite) LocalStacks(ctx context.Context) ([]store.LocalStack, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+localStackColumns+` FROM local_stacks ORDER BY host_id, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectLocalStacks(rows)
}

func collectLocalStacks(rows *sql.Rows) ([]store.LocalStack, error) {
	out := []store.LocalStack{}
	for rows.Next() {
		st, err := scanLocalStack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
