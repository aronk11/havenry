package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aronk11/havenry/internal/store"
)

// teamMigrations werden an migrations angehängt (siehe sqlite.go).
var teamMigrations = []string{
	`CREATE TABLE IF NOT EXISTS teams (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '',
		role        TEXT NOT NULL,
		host_ids    TEXT NOT NULL DEFAULT '',
		created_at  INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS team_members (
		team_id   TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
		user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at INTEGER NOT NULL,
		PRIMARY KEY (team_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_team_members_user ON team_members(user_id)`,
}

func (s *SQLite) CreateTeam(ctx context.Context, t store.Team) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, description, role, host_ids, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Description, t.Role, encodeHostIDs(t.HostIDs), t.CreatedAt.UnixMilli())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return store.ErrTeamExists
	}
	return err
}

func (s *SQLite) UpdateTeam(ctx context.Context, t store.Team) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE teams SET name = ?, description = ?, role = ?, host_ids = ? WHERE id = ?`,
		t.Name, t.Description, t.Role, encodeHostIDs(t.HostIDs), t.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return store.ErrTeamExists
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLite) DeleteTeam(ctx context.Context, id string) error {
	// Mitgliedschaften hängen per ON DELETE CASCADE daran.
	res, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

const teamColumns = `id, name, description, role, host_ids, created_at`

func scanTeam(row interface{ Scan(...any) error }) (store.Team, error) {
	var t store.Team
	var hostIDs string
	var created int64
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Role, &hostIDs, &created); err != nil {
		return store.Team{}, err
	}
	t.HostIDs = decodeHostIDs(hostIDs)
	t.CreatedAt = time.UnixMilli(created).UTC()
	return t, nil
}

func (s *SQLite) TeamByID(ctx context.Context, id string) (store.Team, error) {
	t, err := scanTeam(s.db.QueryRowContext(ctx, `SELECT `+teamColumns+` FROM teams WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Team{}, store.ErrNotFound
	}
	return t, err
}

func (s *SQLite) Teams(ctx context.Context) ([]store.Team, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+teamColumns+` FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AddTeamMember ist absichtlich idempotent: Jemanden zweimal hinzuzufügen ist
// kein Fehler, den ein Bediener auflösen müsste.
func (s *SQLite) AddTeamMember(ctx context.Context, teamID, userID string, at time.Time) error {
	if _, err := s.TeamByID(ctx, teamID); err != nil {
		return err
	}
	if _, err := s.UserByID(ctx, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, joined_at) VALUES (?, ?, ?)
		 ON CONFLICT(team_id, user_id) DO NOTHING`,
		teamID, userID, at.UnixMilli())
	return err
}

func (s *SQLite) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLite) TeamMembers(ctx context.Context, teamID string) ([]store.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.role, u.host_ids,
		       u.created_at, u.last_login, u.must_change_password
		FROM users u
		JOIN team_members m ON m.user_id = u.id
		WHERE m.team_id = ?
		ORDER BY u.username`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLite) TeamsForUser(ctx context.Context, userID string) ([]store.Team, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.description, t.role, t.host_ids, t.created_at
		FROM teams t
		JOIN team_members m ON m.team_id = t.id
		WHERE m.user_id = ?
		ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
