package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aronk11/havenry/internal/store"
)

// SQLite ist die persistente Implementierung von store.Store (ADR-0005).
//
// Bewusst reines database/sql mit handgeschriebenem SQL: kein ORM, keine
// Codegenerierung. Das Schema ist klein und bleibt es — der Inhalt ist
// ausschließlich abgeleiteter Zustand (ADR-0002).
type SQLite struct {
	db *sql.DB
}

// Beim Import registriert sich das Backend für das Schema "sqlite" (ADR-0031).
// Der Aufrufer bindet es mit einem blank import ein und wählt es über die DSN.
func init() {
	store.Register("sqlite", func(ctx context.Context, dsn string) (store.Full, error) {
		return OpenSQLite(ctx, dsn)
	})
}

// OpenSQLite öffnet die Datenbank und führt die Migrationen aus.
func OpenSQLite(ctx context.Context, dsn string) (*SQLite, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("datenbank öffnen: %w", err)
	}

	// SQLite verträgt genau einen Schreiber. Mehr Verbindungen bringen keine
	// Parallelität, sondern nur "database is locked".
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",  // Leser blockieren den Schreiber nicht
		"PRAGMA busy_timeout = 5000", // kurze Sperren aussitzen statt scheitern
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL", // mit WAL ausreichend, deutlich schneller
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	s := &SQLite{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

// migrations werden in Reihenfolge angewendet. Neue Migrationen werden
// angehängt, bestehende nie geändert.
//
// Die Liste wird in allMigrations() zusammengesetzt statt von init()-Funktionen
// befüllt. Der frühere Weg war reihenfolgeabhängig und unsichtbar: Wer die
// Liste las, sah nicht, dass anderswo etwas angehängt wurde.
var coreMigrations = []string{
	`CREATE TABLE IF NOT EXISTS hosts (
		id              TEXT PRIMARY KEY,
		hostname        TEXT NOT NULL,
		credential_hash TEXT NOT NULL UNIQUE,
		approved        INTEGER NOT NULL DEFAULT 0,
		os              TEXT NOT NULL DEFAULT '',
		arch            TEXT NOT NULL DEFAULT '',
		agent_version   TEXT NOT NULL DEFAULT '',
		enrolled_at     INTEGER NOT NULL,
		last_seen       INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS enroll_tokens (
		token_hash TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		used_at    INTEGER
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		at      INTEGER NOT NULL,
		host_id TEXT NOT NULL DEFAULT '',
		kind    TEXT NOT NULL,
		actor   TEXT NOT NULL,
		summary TEXT NOT NULL,
		details TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_events_at ON events(at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_hosts_cred ON hosts(credential_hash)`,
}

// allMigrations setzt das Schema in fester, sichtbarer Reihenfolge zusammen.
//
// Reihenfolge und Inhalt bestehender Einträge bleiben unverändert — nur so
// bleiben bestehende Datenbanken aktualisierbar. Neues wird angehängt.
func allMigrations() []string {
	out := make([]string, 0, len(coreMigrations)+len(userMigrations)+len(teamMigrations)+len(localStackMigrations))
	out = append(out, coreMigrations...)
	out = append(out, userMigrations...)
	out = append(out, teamMigrations...)
	out = append(out, localStackMigrations...)
	return out
}

func (s *SQLite) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("versionstabelle: %w", err)
	}

	var current int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return err
		}
		current = 0
	} else if err != nil {
		return fmt.Errorf("schema-version lesen: %w", err)
	}

	migrations := allMigrations()

	if current > len(migrations) {
		// Die Datenbank stammt aus einer neueren Version. Weiterarbeiten würde
		// Daten beschädigen — lieber klar scheitern.
		return fmt.Errorf("datenbank hat schema-version %d, dieses binary kennt nur %d — bitte aktualisieren",
			current, len(migrations))
	}

	for i := current; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, i+1); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) CreateEnrollToken(ctx context.Context, t store.EnrollToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enroll_tokens (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		t.TokenHash, t.CreatedAt.UnixMilli(), t.ExpiresAt.UnixMilli())
	return err
}

// ConsumeEnrollToken entwertet ein Token atomar.
//
// Prüfung und Entwertung stehen bewusst in EINEM UPDATE mit der Bedingung
// used_at IS NULL: Zwei Agenten, die dasselbe Token gleichzeitig einlösen,
// können sonst beide durchkommen. Die Anzahl betroffener Zeilen entscheidet.
func (s *SQLite) ConsumeEnrollToken(ctx context.Context, hash string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE enroll_tokens SET used_at = ?
		 WHERE token_hash = ? AND used_at IS NULL AND expires_at >= ?`,
		now.UnixMilli(), hash, now.UnixMilli())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}

	// Nichts geändert — jetzt erst den Grund ermitteln, für eine hilfreiche Meldung.
	var expiresAt int64
	var usedAt sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		`SELECT expires_at, used_at FROM enroll_tokens WHERE token_hash = ?`, hash).
		Scan(&expiresAt, &usedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return store.ErrNotFound
	case err != nil:
		return err
	case usedAt.Valid:
		return store.ErrTokenUsed
	default:
		return store.ErrTokenExpired
	}
}

func (s *SQLite) UpsertHost(ctx context.Context, h store.Host) error {
	// approved und enrolled_at bleiben beim Upsert erhalten: eine Neuverbindung
	// darf einen bestätigten store.Host nicht auf unbestätigt zurücksetzen.
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hosts (id, hostname, credential_hash, approved, os, arch, agent_version, enrolled_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hostname      = excluded.hostname,
			os            = excluded.os,
			arch          = excluded.arch,
			agent_version = excluded.agent_version,
			last_seen     = excluded.last_seen`,
		h.ID, h.Hostname, h.CredentialHash, boolToInt(h.Approved),
		h.OS, h.Arch, h.AgentVersion, h.EnrolledAt.UnixMilli(), h.LastSeen.UnixMilli())
	return err
}

const hostColumns = `id, hostname, credential_hash, approved, os, arch, agent_version, enrolled_at, last_seen`

func scanHost(row interface{ Scan(...any) error }) (store.Host, error) {
	var h store.Host
	var approved int
	var enrolled, lastSeen int64
	err := row.Scan(&h.ID, &h.Hostname, &h.CredentialHash, &approved,
		&h.OS, &h.Arch, &h.AgentVersion, &enrolled, &lastSeen)
	if err != nil {
		return store.Host{}, err
	}
	h.Approved = approved != 0
	h.EnrolledAt = time.UnixMilli(enrolled).UTC()
	h.LastSeen = time.UnixMilli(lastSeen).UTC()
	return h, nil
}

func (s *SQLite) HostByID(ctx context.Context, id string) (store.Host, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+hostColumns+` FROM hosts WHERE id = ?`, id)
	h, err := scanHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Host{}, store.ErrNotFound
	}
	return h, err
}

func (s *SQLite) HostByCredentialHash(ctx context.Context, hash string) (store.Host, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+hostColumns+` FROM hosts WHERE credential_hash = ?`, hash)
	h, err := scanHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Host{}, store.ErrNotFound
	}
	return h, err
}

func (s *SQLite) Hosts(ctx context.Context) ([]store.Host, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+hostColumns+` FROM hosts ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *SQLite) ApproveHost(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE hosts SET approved = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLite) TouchHost(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE hosts SET last_seen = ? WHERE id = ?`, at.UnixMilli(), id)
	return err
}

func (s *SQLite) AppendEvent(ctx context.Context, e store.Event) error {
	var details any
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return err
		}
		details = string(b)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (at, host_id, kind, actor, summary, details) VALUES (?, ?, ?, ?, ?, ?)`,
		e.At.UnixMilli(), e.HostID, e.Kind, e.Actor, e.Summary, details)
	return err
}

// Events liefert die neuesten Ereignisse, aufsteigend sortiert.
func (s *SQLite) Events(ctx context.Context, limit int) ([]store.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, host_id, kind, actor, summary, details
		 FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Event
	for rows.Next() {
		var e store.Event
		var at int64
		var details sql.NullString
		if err := rows.Scan(&e.ID, &at, &e.HostID, &e.Kind, &e.Actor, &e.Summary, &details); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(at).UTC()
		if details.Valid && details.String != "" {
			_ = json.Unmarshal([]byte(details.String), &e.Details)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Abfrage war absteigend (neueste zuerst), Rückgabe ist aufsteigend.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ store.Store = (*SQLite)(nil)
