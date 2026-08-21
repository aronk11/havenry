// Package gitsync hält eine lokale Arbeitskopie des Nutzer-Repos aktuell.
//
// Das Repo ist die Quelle des Soll-Zustands (ADR-0002). Gearbeitet wird mit dem
// git-Binary statt einer Bibliothek (ADR-0021).
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout begrenzt jeden git-Aufruf.
//
// Ohne Zeitlimit kann ein hängender Netzwerkaufruf die Synchronisation
// dauerhaft blockieren — im Homelab keineswegs selten (Repo hinter VPN, das
// gerade nicht steht).
const DefaultTimeout = 2 * time.Minute

// Config beschreibt das zu synchronisierende Repo.
type Config struct {
	URL    string
	Branch string
	// WorkDir ist das Verzeichnis der Arbeitskopie.
	WorkDir string
	// SSHKeyPath verweist auf einen Deploy-Key. Leer = Standardverhalten von git.
	SSHKeyPath string
	Timeout    time.Duration
}

// Syncer hält eine Arbeitskopie aktuell.
type Syncer struct {
	cfg Config

	// mu serialisiert git-Aufrufe. Zwei gleichzeitige Fetches auf dasselbe
	// Verzeichnis erzeugen Sperrdateien-Konflikte in .git.
	mu sync.Mutex
}

func New(cfg Config) *Syncer {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	return &Syncer{cfg: cfg}
}

// ErrGitMissing bedeutet, dass das git-Binary nicht gefunden wurde.
var ErrGitMissing = errors.New("git ist nicht installiert oder nicht im PATH (siehe ADR-0021)")

// CheckGitAvailable prüft die Voraussetzung aus ADR-0021.
// Wird beim Start aufgerufen, damit ein fehlendes git sofort auffällt.
func CheckGitAvailable() (string, error) {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return "", ErrGitMissing
	}
	return strings.TrimSpace(string(out)), nil
}

// Result beschreibt das Ergebnis einer Synchronisation.
type Result struct {
	Commit  string
	Subject string
	// Changed meldet, ob sich der Commit gegenüber dem vorigen Lauf geändert hat.
	Changed bool
}

// Sync stellt sicher, dass die Arbeitskopie auf dem aktuellen Stand des
// konfigurierten Branches steht.
//
// Beim ersten Aufruf wird geklont, danach geholt und hart zurückgesetzt.
// Hartes Zurücksetzen ist hier richtig: Die Arbeitskopie gehört der Plattform,
// nicht dem Nutzer — sie ist ein Abbild des Repos, kein Arbeitsplatz. Der
// Nutzer arbeitet in seinem eigenen Klon.
func (s *Syncer) Sync(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	before, _ := s.headCommit(ctx)

	gitDir := filepath.Join(s.cfg.WorkDir, ".git")
	if _, err := os.Stat(gitDir); errors.Is(err, os.ErrNotExist) {
		if err := s.clone(ctx); err != nil {
			return Result{}, err
		}
	} else {
		if err := s.fetchAndReset(ctx); err != nil {
			return Result{}, err
		}
	}

	commit, err := s.headCommit(ctx)
	if err != nil {
		return Result{}, err
	}
	subject, _ := s.run(ctx, "log", "-1", "--pretty=%s")

	return Result{
		Commit:  commit,
		Subject: strings.TrimSpace(subject),
		Changed: before != commit,
	}, nil
}

func (s *Syncer) clone(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.WorkDir), 0o700); err != nil {
		return fmt.Errorf("arbeitsverzeichnis anlegen: %w", err)
	}
	// Ein bestehendes, aber unvollständiges Verzeichnis würde clone scheitern
	// lassen. Da die Arbeitskopie jederzeit wegwerfbar ist, wird sie geleert.
	_ = os.RemoveAll(s.cfg.WorkDir)

	// --depth 1: Wir brauchen nur den aktuellen Stand. Bei einem Repo mit
	// langer Historie spart das erheblich Zeit und Platz.
	_, err := s.runIn(ctx, filepath.Dir(s.cfg.WorkDir),
		"clone", "--depth", "1", "--branch", s.cfg.Branch,
		"--single-branch", s.cfg.URL, s.cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("repo klonen: %w", err)
	}
	return nil
}

func (s *Syncer) fetchAndReset(ctx context.Context) error {
	if _, err := s.run(ctx, "fetch", "--depth", "1", "origin", s.cfg.Branch); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if _, err := s.run(ctx, "reset", "--hard", "origin/"+s.cfg.Branch); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	// Dateien, die aus dem Repo entfernt wurden, müssen auch lokal verschwinden,
	// sonst zeigt die Plattform Stacks an, die es nicht mehr gibt.
	if _, err := s.run(ctx, "clean", "-fd"); err != nil {
		return fmt.Errorf("clean: %w", err)
	}
	return nil
}

func (s *Syncer) headCommit(ctx context.Context) (string, error) {
	out, err := s.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *Syncer) run(ctx context.Context, args ...string) (string, error) {
	return s.runIn(ctx, s.cfg.WorkDir, args...)
}

// runIn führt git aus.
//
// Argumente werden als Liste übergeben, nie über eine Shell — eine Repo-URL
// mit Sonderzeichen kann damit nicht zu einem Befehl werden.
func (s *Syncer) runIn(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	// Minimale, kontrollierte Umgebung.
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		// Verhindert, dass git auf eine Passworteingabe wartet und dabei
		// stillsteht, bis das Zeitlimit greift.
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C",
	}
	if s.cfg.SSHKeyPath != "" {
		// StrictHostKeyChecking bleibt aktiv: Ein Deploy-Key schützt nichts,
		// wenn die Gegenstelle beliebig sein darf.
		env = append(env, fmt.Sprintf(
			"GIT_SSH_COMMAND=ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new",
			s.cfg.SSHKeyPath))
	}
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: zeitüberschreitung nach %s (%s)",
				args[0], s.cfg.Timeout, msg)
		}
		// Die Meldung von git wird durchgereicht: Sie ist für diese Zielgruppe
		// aussagekräftiger als alles, was wir daraus machen würden.
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}
