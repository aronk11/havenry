package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aronk11/havenry/internal/store"
)

// userMigrations ergänzen das Schema aus sqlite.go. Sie werden dort an
// migrations angehängt — bestehende Migrationen bleiben unverändert.
var userMigrations = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id                   TEXT PRIMARY KEY,
		username             TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password_hash        TEXT NOT NULL,
		role                 TEXT NOT NULL,
		host_ids             TEXT NOT NULL DEFAULT '',
		created_at           INTEGER NOT NULL,
		last_login           INTEGER,
		must_change_password INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		token_hash TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
	`CREATE TABLE IF NOT EXISTS api_tokens (
		id         TEXT PRIMARY KEY,
		token_hash TEXT NOT NULL UNIQUE,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER,
		last_used  INTEGER
	)`,
	`CREATE TABLE IF NOT EXISTS git_repo (
		singleton     INTEGER PRIMARY KEY CHECK (singleton = 1),
		url           TEXT NOT NULL,
		branch        TEXT NOT NULL,
		base_path     TEXT NOT NULL DEFAULT '',
		ssh_key_path  TEXT NOT NULL DEFAULT '',
		last_sync     INTEGER,
		last_commit   TEXT NOT NULL DEFAULT '',
		last_error    TEXT NOT NULL DEFAULT '',
		configured_at INTEGER NOT NULL
	)`,
}

// hostIDs werden als kommagetrennte Liste abgelegt.
//
// Eine eigene Zuordnungstabelle wäre sauberer normalisiert, aber die Liste ist
// pro Nutzer kurz, wird immer vollständig gelesen und nie einzeln abgefragt.
// Für diese Datenmenge wäre die Tabelle Aufwand ohne Nutzen.
func encodeHostIDs(ids []string) string { return strings.Join(ids, ",") }

func decodeHostIDs(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (s *SQLite) CreateUser(ctx context.Context, u store.User) error {
	var lastLogin any
	if u.LastLogin != nil {
		lastLogin = u.LastLogin.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, host_ids, created_at, last_login, must_change_password)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.PasswordHash, u.Role, encodeHostIDs(u.HostIDs),
		u.CreatedAt.UnixMilli(), lastLogin, boolToInt(u.MustChangePassword))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return store.ErrUserExists
	}
	return err
}

func (s *SQLite) UpdateUser(ctx context.Context, u store.User) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET username = ?, password_hash = ?, role = ?, host_ids = ?, must_change_password = ?
		WHERE id = ?`,
		u.Username, u.PasswordHash, u.Role, encodeHostIDs(u.HostIDs),
		boolToInt(u.MustChangePassword), u.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return store.ErrUserExists
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLite) DeleteUser(ctx context.Context, id string) error {
	// Sitzungen und Token hängen per ON DELETE CASCADE daran: Wer gelöscht
	// wird, ist sofort überall abgemeldet.
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

const userColumns = `id, username, password_hash, role, host_ids, created_at, last_login, must_change_password`

func scanUser(row interface{ Scan(...any) error }) (store.User, error) {
	var u store.User
	var hostIDs string
	var createdAt int64
	var lastLogin sql.NullInt64
	var mustChange int

	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &hostIDs,
		&createdAt, &lastLogin, &mustChange); err != nil {
		return store.User{}, err
	}
	u.HostIDs = decodeHostIDs(hostIDs)
	u.CreatedAt = time.UnixMilli(createdAt).UTC()
	u.MustChangePassword = mustChange != 0
	if lastLogin.Valid {
		t := time.UnixMilli(lastLogin.Int64).UTC()
		u.LastLogin = &t
	}
	return u, nil
}

func (s *SQLite) UserByID(ctx context.Context, id string) (store.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	return u, err
}

func (s *SQLite) UserByName(ctx context.Context, name string) (store.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = ? COLLATE NOCASE`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	return u, err
}

func (s *SQLite) Users(ctx context.Context) ([]store.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY username`)
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

func (s *SQLite) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *SQLite) TouchLogin(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login = ? WHERE id = ?`, at.UnixMilli(), id)
	return err
}

func (s *SQLite) CreateSession(ctx context.Context, sess store.Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		sess.TokenHash, sess.UserID, sess.CreatedAt.UnixMilli(), sess.ExpiresAt.UnixMilli())
	return err
}

// SessionByHash liefert eine Sitzung. Abgelaufene Sitzungen gelten als
// nicht vorhanden — die Prüfung gehört in die Abfrage, nicht in den Aufrufer.
func (s *SQLite) SessionByHash(ctx context.Context, hash string) (store.Session, error) {
	var sess store.Session
	var created, expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, created_at, expires_at FROM sessions
		 WHERE token_hash = ? AND expires_at > ?`, hash, time.Now().UnixMilli()).
		Scan(&sess.TokenHash, &sess.UserID, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, store.ErrNotFound
	}
	if err != nil {
		return store.Session{}, err
	}
	sess.CreatedAt = time.UnixMilli(created).UTC()
	sess.ExpiresAt = time.UnixMilli(expires).UTC()
	return sess, nil
}

func (s *SQLite) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

// DeleteUserSessions meldet einen Nutzer überall ab. Wird bei Rollenwechsel
// und Passwortänderung aufgerufen: Eine alte Sitzung darf keine alten Rechte
// weitertragen.
func (s *SQLite) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *SQLite) PurgeExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UnixMilli())
	return err
}

func (s *SQLite) CreateAPIToken(ctx context.Context, t store.APIToken) error {
	var expires any
	if t.ExpiresAt != nil {
		expires = t.ExpiresAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, token_hash, user_id, name, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.TokenHash, t.UserID, t.Name, t.CreatedAt.UnixMilli(), expires)
	return err
}

func (s *SQLite) APITokenByHash(ctx context.Context, hash string) (store.APIToken, error) {
	var t store.APIToken
	var created int64
	var expires, lastUsed sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, token_hash, user_id, name, created_at, expires_at, last_used
		 FROM api_tokens WHERE token_hash = ?`, hash).
		Scan(&t.ID, &t.TokenHash, &t.UserID, &t.Name, &created, &expires, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return store.APIToken{}, store.ErrNotFound
	}
	if err != nil {
		return store.APIToken{}, err
	}
	t.CreatedAt = time.UnixMilli(created).UTC()
	if expires.Valid {
		e := time.UnixMilli(expires.Int64).UTC()
		t.ExpiresAt = &e
		if time.Now().After(e) {
			return store.APIToken{}, store.ErrTokenExpired
		}
	}
	if lastUsed.Valid {
		l := time.UnixMilli(lastUsed.Int64).UTC()
		t.LastUsed = &l
	}
	return t, nil
}

func (s *SQLite) APITokensByUser(ctx context.Context, userID string) ([]store.APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, token_hash, user_id, name, created_at, expires_at, last_used
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.APIToken
	for rows.Next() {
		var t store.APIToken
		var created int64
		var expires, lastUsed sql.NullInt64
		if err := rows.Scan(&t.ID, &t.TokenHash, &t.UserID, &t.Name, &created, &expires, &lastUsed); err != nil {
			return nil, err
		}
		t.CreatedAt = time.UnixMilli(created).UTC()
		if expires.Valid {
			e := time.UnixMilli(expires.Int64).UTC()
			t.ExpiresAt = &e
		}
		if lastUsed.Valid {
			l := time.UnixMilli(lastUsed.Int64).UTC()
			t.LastUsed = &l
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteAPIToken(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLite) TouchAPIToken(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used = ? WHERE id = ?`, at.UnixMilli(), id)
	return err
}

func (s *SQLite) SaveRepo(ctx context.Context, r store.GitRepo) error {
	var lastSync any
	if r.LastSync != nil {
		lastSync = r.LastSync.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO git_repo (singleton, url, branch, base_path, ssh_key_path, last_sync, last_commit, last_error, configured_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET
			url = excluded.url, branch = excluded.branch, base_path = excluded.base_path,
			ssh_key_path = excluded.ssh_key_path, last_sync = excluded.last_sync,
			last_commit = excluded.last_commit, last_error = excluded.last_error`,
		r.URL, r.Branch, r.BasePath, r.SSHKeyPath, lastSync, r.LastCommit, r.LastError,
		r.ConfiguredAt.UnixMilli())
	return err
}

func (s *SQLite) Repo(ctx context.Context) (store.GitRepo, error) {
	var r store.GitRepo
	var lastSync sql.NullInt64
	var configuredAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT url, branch, base_path, ssh_key_path, last_sync, last_commit, last_error, configured_at
		 FROM git_repo WHERE singleton = 1`).
		Scan(&r.URL, &r.Branch, &r.BasePath, &r.SSHKeyPath, &lastSync, &r.LastCommit, &r.LastError, &configuredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.GitRepo{}, store.ErrNotFound
	}
	if err != nil {
		return store.GitRepo{}, err
	}
	r.ConfiguredAt = time.UnixMilli(configuredAt).UTC()
	if lastSync.Valid {
		t := time.UnixMilli(lastSync.Int64).UTC()
		r.LastSync = &t
	}
	return r, nil
}

func (s *SQLite) ClearRepo(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM git_repo WHERE singleton = 1`)
	return err
}

var _ store.Full = (*SQLite)(nil)
