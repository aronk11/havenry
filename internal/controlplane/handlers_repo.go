package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/aronk11/havenry/internal/gitsync"
	"github.com/aronk11/havenry/internal/reconcile"
	"github.com/aronk11/havenry/internal/store"
)

// SyncInterval ist der Takt der Repo-Synchronisation.
//
// 60 Sekunden ist ein Kompromiss: schnell genug, dass ein Push spürbar
// ankommt, langsam genug, dass ein privat gehostetes Git nicht unnötig
// belastet wird. Ein Webhook-Endpunkt für sofortige Synchronisation ist
// ein späterer Zusatz.
const SyncInterval = 60 * time.Second

// repoManager hält die Arbeitskopie aktuell und kennt die erkannten Stacks.
type repoManager struct {
	store   store.Full
	workDir string
	logger  *slog.Logger

	mu        sync.RWMutex
	syncer    *gitsync.Syncer
	discovery gitsync.Discovery
	lastSync  time.Time
	lastError string
	commit    string
	subject   string
	// canPush und pushCheckedAt speichern das Ergebnis der
	// Schreibrechtsprüfung zwischen (siehe CanPush).
	canPush       bool
	pushCheckedAt time.Time
}

func newRepoManager(s store.Full, workDir string, logger *slog.Logger) *repoManager {
	return &repoManager{store: s, workDir: workDir, logger: logger}
}

// Configure setzt das Repo und synchronisiert sofort.
func (m *repoManager) Configure(ctx context.Context, r store.GitRepo) error {
	m.mu.Lock()
	m.syncer = gitsync.New(gitsync.Config{
		URL: r.URL, Branch: r.Branch,
		WorkDir: m.workDir, SSHKeyPath: r.SSHKeyPath,
	})
	// Ein neues Repo bedeutet neue Berechtigungen.
	m.pushCheckedAt = time.Time{}
	m.mu.Unlock()

	if err := m.store.SaveRepo(ctx, r); err != nil {
		return err
	}
	return m.SyncNow(ctx)
}

// Restore lädt eine zuvor gespeicherte Konfiguration beim Start.
func (m *repoManager) Restore(ctx context.Context) error {
	r, err := m.store.Repo(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.syncer = gitsync.New(gitsync.Config{
		URL: r.URL, Branch: r.Branch,
		WorkDir: m.workDir, SSHKeyPath: r.SSHKeyPath,
	})
	m.mu.Unlock()
	return nil
}

// SyncNow holt den aktuellen Stand und erkennt die Stacks neu.
func (m *repoManager) SyncNow(ctx context.Context) error {
	m.mu.RLock()
	syncer := m.syncer
	m.mu.RUnlock()
	if syncer == nil {
		return errors.New("kein repository konfiguriert")
	}

	repo, err := m.store.Repo(ctx)
	if err != nil {
		return err
	}

	res, syncErr := syncer.Sync(ctx)
	now := time.Now().UTC()

	if syncErr != nil {
		m.mu.Lock()
		m.lastError = syncErr.Error()
		m.mu.Unlock()

		repo.LastError = syncErr.Error()
		repo.LastSync = &now
		_ = m.store.SaveRepo(ctx, repo)
		return syncErr
	}

	// Erkennung läuft auch dann weiter, wenn einzelne Stacks Probleme haben —
	// die werden gesammelt und angezeigt, nicht geworfen.
	disc, discErr := gitsync.Discover(m.workDir, repo.BasePath)

	m.mu.Lock()
	m.discovery = disc
	m.lastSync = now
	m.commit = res.Commit
	m.subject = res.Subject
	m.lastError = ""
	if discErr != nil {
		m.lastError = discErr.Error()
	}
	m.mu.Unlock()

	repo.LastSync = &now
	repo.LastCommit = res.Commit
	repo.LastError = ""
	if discErr != nil {
		repo.LastError = discErr.Error()
	}
	_ = m.store.SaveRepo(ctx, repo)

	if res.Changed {
		_ = m.store.AppendEvent(ctx, store.Event{
			At: now, Kind: "repo.synced", Actor: "system",
			Summary: fmt.Sprintf("Neuer Stand: %s", res.Subject),
			Details: map[string]string{
				"commit": shortID(res.Commit),
				"stacks": fmt.Sprint(len(disc.Stacks)),
			},
		})
		m.logger.Info("repo aktualisiert",
			"commit", shortID(res.Commit), "stacks", len(disc.Stacks), "probleme", len(disc.Problems))
	}
	return discErr
}

// Run startet die Synchronisationsschleife.
func (m *repoManager) Run(ctx context.Context) {
	t := time.NewTicker(SyncInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.RLock()
			configured := m.syncer != nil
			m.mu.RUnlock()
			if !configured {
				continue
			}
			if err := m.SyncNow(ctx); err != nil {
				m.logger.Warn("repo-synchronisation fehlgeschlagen", "fehler", err)
			}
		}
	}
}

// Snapshot liefert den aktuellen Erkennungsstand.
func (m *repoManager) Snapshot() (gitsync.Discovery, time.Time, string, string, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.discovery, m.lastSync, m.commit, m.subject, m.lastError
}

// StacksForHost liefert die Soll-Stacks eines Hosts.
func (m *repoManager) StacksForHost(hostname string) []gitsync.Stack {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []gitsync.Stack
	for _, s := range m.discovery.Stacks {
		for _, h := range s.Hosts {
			if h == hostname {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// --- HTTP-Handler ---

func (s *Server) getRepo(w http.ResponseWriter, r *http.Request) {
	disc, lastSync, commit, subject, lastErr := s.repo.Snapshot()

	repo, err := s.store.Repo(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	type stackView struct {
		Name        string   `json:"name"`
		Hosts       []string `json:"hosts"`
		ComposePath string   `json:"compose_path"`
		Mode        string   `json:"mode"`
		Updates     string   `json:"updates"`
		EnvExample  []string `json:"env_example,omitempty"`
	}
	stacks := make([]stackView, 0, len(disc.Stacks))
	for _, st := range disc.Stacks {
		stacks = append(stacks, stackView{
			Name: st.Name, Hosts: st.Hosts, ComposePath: st.ComposePath,
			Mode: string(st.Mode), Updates: string(st.Updates), EnvExample: st.EnvExample,
		})
	}
	problems := disc.Problems
	if problems == nil {
		problems = []gitsync.Problem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":     true,
		"url":            repo.URL,
		"branch":         repo.Branch,
		"base_path":      repo.BasePath,
		"last_sync":      lastSync,
		"last_commit":    shortID(commit),
		"last_subject":   subject,
		"last_error":     lastErr,
		"desired_stacks": stacks,
		"problems":       problems,
	})
}

func (s *Server) setRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL        string `json:"url"`
		Branch     string `json:"branch"`
		BasePath   string `json:"base_path"`
		SSHKeyPath string `json:"ssh_key_path"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.URL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("url ist erforderlich"))
		return
	}
	if body.Branch == "" {
		body.Branch = "main"
	}
	// Ein Pfad, der aus dem Repo herausführt, wird abgelehnt.
	if filepath.IsAbs(body.BasePath) || filepath.Clean(body.BasePath) == ".." ||
		len(body.BasePath) > 0 && filepath.Clean(body.BasePath)[0] == '.' && len(filepath.Clean(body.BasePath)) > 1 {
		writeErr(w, http.StatusBadRequest, errors.New("base_path muss ein relativer pfad innerhalb des repos sein"))
		return
	}

	id := identityFrom(r.Context())
	repo := store.GitRepo{
		URL: body.URL, Branch: body.Branch, BasePath: body.BasePath,
		SSHKeyPath: body.SSHKeyPath, ConfiguredAt: time.Now().UTC(),
	}

	if err := s.repo.Configure(r.Context(), repo); err != nil {
		// Konfiguration bleibt gespeichert, damit der Nutzer sie korrigieren
		// kann statt sie neu einzugeben — der Fehler wird angezeigt.
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"configured": true,
			"error":      err.Error(),
			"hinweis":    "Konfiguration gespeichert, aber die Synchronisation schlug fehl.",
		})
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "repo.configured", Actor: id.Actor(),
		Summary: fmt.Sprintf("Repository gesetzt: %s (%s)", body.URL, body.Branch),
	})
	s.getRepo(w, r)
}

func (s *Server) syncRepo(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.SyncNow(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.getRepo(w, r)
}

func (s *Server) deleteRepo(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	if err := s.store.ClearRepo(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "repo.removed", Actor: id.Actor(),
		Summary: "Repository-Verknüpfung entfernt",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "entfernt"})
}

// AdoptImage schreibt eine geänderte Image-Angabe ins Repo (ADR-0028).
//
// Nach der Änderung wird geprüft, ob die Datei noch gültiges Compose ist und
// ob genau die erwartete Änderung entstanden ist. Schlägt das fehl, wird
// nichts geschrieben — eine beschädigte Compose-Datei wäre der schlimmste
// mögliche Ausgang.
func (m *repoManager) AdoptImage(ctx context.Context, relPath, service, newImage, author string) (string, error) {
	m.mu.RLock()
	syncer := m.syncer
	m.mu.RUnlock()
	if syncer == nil {
		return "", errors.New("kein repository konfiguriert")
	}

	// Lesen, Ändern, Committen und Pushen laufen unter derselben Sperre wie
	// die Synchronisation — sonst könnte der 60-Sekunden-Takt dazwischen ein
	// `reset --hard` ausführen und die Änderung spurlos verwerfen.
	commit, err := syncer.EditAndPush(ctx, relPath,
		fmt.Sprintf("%s: image auf %s gesetzt", service, newImage),
		author,
		func(current []byte) ([]byte, error) {
			updated, err := gitsync.SetServiceImage(current, service, newImage)
			if err != nil {
				return nil, err
			}
			// Gegenprobe: Ist das Ergebnis noch gültiges Compose, und steht
			// dort wirklich das erwartete Image? Eine beschädigte
			// Compose-Datei wäre der schlimmste mögliche Ausgang (ADR-0028).
			parsed, err := reconcile.ParseCompose("pruefung", updated)
			if err != nil {
				return nil, fmt.Errorf("die änderung hätte die compose-datei beschädigt, nichts geschrieben: %w", err)
			}
			svc, ok := parsed.Desired.Services[service]
			if !ok {
				return nil, fmt.Errorf("dienst %q nach der änderung nicht mehr auffindbar, nichts geschrieben", service)
			}
			if svc.Image != reconcile.NormalizeImage(newImage) {
				return nil, fmt.Errorf("die änderung hat nicht gegriffen (erwartet %q, gefunden %q), nichts geschrieben",
					reconcile.NormalizeImage(newImage), svc.Image)
			}
			return updated, nil
		})
	if err != nil {
		return "", err
	}

	// Der Push-Zustand kann sich geändert haben; außerdem soll die Anzeige
	// sofort den neuen Commit zeigen.
	m.invalidatePushCache()
	if err := m.SyncNow(ctx); err != nil {
		m.logger.Warn("nach adopt konnte nicht neu synchronisiert werden", "fehler", err)
	}
	return commit, nil
}

// PushCheckTTL bestimmt, wie lange das Ergebnis der Schreibrechtsprüfung gilt.
//
// Die Oberfläche ruft die Drift-Ansicht alle fünf Sekunden ab. Ohne
// Zwischenspeicher wäre das alle fünf Sekunden ein `git push --dry-run` gegen
// die Gegenstelle — pro angemeldetem Nutzer. Bei GitHub führt das zügig zu
// einer Drosselung, bei einem selbst gehosteten Git zu unnötiger Last.
const PushCheckTTL = 5 * time.Minute

// CanPush meldet, ob Schreibzugriff aufs Repo besteht.
//
// Das Ergebnis wird zwischengespeichert; die Prüfung selbst ist ein
// Netzwerkaufruf.
func (m *repoManager) CanPush(ctx context.Context) (bool, time.Time) {
	m.mu.RLock()
	syncer := m.syncer
	cached, checkedAt := m.canPush, m.pushCheckedAt
	m.mu.RUnlock()

	if syncer == nil {
		return false, time.Time{}
	}
	if !checkedAt.IsZero() && time.Since(checkedAt) < PushCheckTTL {
		return cached, checkedAt
	}

	ok := syncer.CanPush(ctx)
	now := time.Now().UTC()

	m.mu.Lock()
	m.canPush, m.pushCheckedAt = ok, now
	m.mu.Unlock()
	return ok, now
}

// invalidatePushCache erzwingt eine neue Prüfung beim nächsten Abruf.
func (m *repoManager) invalidatePushCache() {
	m.mu.Lock()
	m.pushCheckedAt = time.Time{}
	m.mu.Unlock()
}
