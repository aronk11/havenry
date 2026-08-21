package gitsync_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aronk11/havenry/internal/gitsync"
)

// newRepo legt ein echtes lokales Git-Repo an.
//
// Bewusst ein echtes Repo statt eines Attrappen-Ablaufs: Getestet wird ja
// gerade das Zusammenspiel mit dem git-Binary (ADR-0021). Ein nachgebauter
// Ablauf würde genau das übergehen, was schiefgehen kann.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost",
			"GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-b", "main")
	return dir
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost",
			"GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

const beispielCompose = `services:
  jellyfin:
    image: jellyfin/jellyfin:10.9
    ports:
      - "8096:8096"
`

func TestSyncClonesAndDetectsChanges(t *testing.T) {
	if _, err := gitsync.CheckGitAvailable(); err != nil {
		t.Skip("git nicht verfügbar")
	}

	origin := newRepo(t)
	write(t, origin, "stacks/nas-01/media/compose.yaml", beispielCompose)
	commit(t, origin, "erster stack")

	work := filepath.Join(t.TempDir(), "arbeitskopie")
	s := gitsync.New(gitsync.Config{URL: origin, Branch: "main", WorkDir: work})

	ctx := context.Background()

	res, err := s.Sync(ctx)
	if err != nil {
		t.Fatalf("erster Sync: %v", err)
	}
	if res.Commit == "" {
		t.Fatal("kein commit gemeldet")
	}
	if !res.Changed {
		t.Error("erster Sync sollte als Änderung gelten")
	}
	if res.Subject != "erster stack" {
		t.Errorf("Subject = %q", res.Subject)
	}

	// Zweiter Lauf ohne Änderung im Origin.
	res2, err := s.Sync(ctx)
	if err != nil {
		t.Fatalf("zweiter Sync: %v", err)
	}
	if res2.Changed {
		t.Error("unveränderter Sync wurde als Änderung gemeldet")
	}

	// Neuer Commit im Origin.
	write(t, origin, "stacks/nas-01/proxy/compose.yaml", "services:\n  caddy:\n    image: caddy:2\n")
	commit(t, origin, "proxy ergaenzt")

	res3, err := s.Sync(ctx)
	if err != nil {
		t.Fatalf("dritter Sync: %v", err)
	}
	if !res3.Changed {
		t.Error("neuer Commit wurde nicht als Änderung erkannt")
	}
	if res3.Commit == res.Commit {
		t.Error("commit-id hat sich nicht geändert")
	}
}

// TestSyncRemovesDeletedFiles prüft, dass gelöschte Stacks auch lokal
// verschwinden. Sonst zeigt die Plattform Stacks an, die es nicht mehr gibt.
func TestSyncRemovesDeletedFiles(t *testing.T) {
	if _, err := gitsync.CheckGitAvailable(); err != nil {
		t.Skip("git nicht verfügbar")
	}

	origin := newRepo(t)
	write(t, origin, "stacks/nas-01/media/compose.yaml", beispielCompose)
	write(t, origin, "stacks/nas-01/alt/compose.yaml", "services:\n  alt:\n    image: alpine\n")
	commit(t, origin, "zwei stacks")

	work := filepath.Join(t.TempDir(), "arbeitskopie")
	s := gitsync.New(gitsync.Config{URL: origin, Branch: "main", WorkDir: work})
	ctx := context.Background()

	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	d, err := gitsync.Discover(work, "")
	if err != nil || len(d.Stacks) != 2 {
		t.Fatalf("%d stacks nach erstem sync, erwartet 2 (%v)", len(d.Stacks), err)
	}

	if err := os.RemoveAll(filepath.Join(origin, "stacks/nas-01/alt")); err != nil {
		t.Fatal(err)
	}
	commit(t, origin, "alten stack entfernt")

	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	d, err = gitsync.Discover(work, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Stacks) != 1 {
		t.Fatalf("%d stacks nach löschung, erwartet 1 — clean hat nicht gegriffen", len(d.Stacks))
	}
}

func TestSyncReportsUsefulError(t *testing.T) {
	if _, err := gitsync.CheckGitAvailable(); err != nil {
		t.Skip("git nicht verfügbar")
	}
	s := gitsync.New(gitsync.Config{
		URL:    filepath.Join(t.TempDir(), "gibt-es-nicht"),
		Branch: "main", WorkDir: filepath.Join(t.TempDir(), "wk"),
	})
	_, err := s.Sync(context.Background())
	if err == nil {
		t.Fatal("sync auf nicht existierendes repo muss scheitern")
	}
	// Die Meldung von git soll durchgereicht werden — sie ist für diese
	// Zielgruppe brauchbarer als eine eigene Umschreibung.
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("fehlermeldung nennt git nicht: %v", err)
	}
}

func TestDiscoverConvention(t *testing.T) {
	root := t.TempDir()
	write(t, root, "stacks/nas-01/media/compose.yaml", beispielCompose)
	write(t, root, "stacks/nas-01/media/.env.example", "# kommentar\nTZ=Europe/Berlin\nPUID=1000\n\nLEER=\n")
	write(t, root, "stacks/pi-02/dns/docker-compose.yml", "services:\n  pihole:\n    image: pihole/pihole\n")

	d, err := gitsync.Discover(root, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(d.Stacks) != 2 {
		t.Fatalf("%d stacks, erwartet 2", len(d.Stacks))
	}
	if len(d.Problems) != 0 {
		t.Fatalf("unerwartete probleme: %+v", d.Problems)
	}

	media := d.Stacks[0]
	if media.Name != "media" || media.Hosts[0] != "nas-01" {
		t.Fatalf("erster stack falsch zugeordnet: %+v", media)
	}
	// Vorgabe ist observe (ADR-0004) — niemals apply ohne ausdrückliche Angabe.
	if media.Mode != gitsync.ModeObserve {
		t.Errorf("Mode = %q, erwartet observe als Vorgabe", media.Mode)
	}
	// Nur Namen, niemals Werte (ADR-0006).
	want := []string{"TZ", "PUID", "LEER"}
	if len(media.EnvExample) != len(want) {
		t.Fatalf("EnvExample = %v, erwartet %v", media.EnvExample, want)
	}
	for _, key := range media.EnvExample {
		if strings.Contains(key, "=") || strings.Contains(key, "Europe") {
			t.Errorf("EnvExample enthält einen Wert statt nur den Namen: %q", key)
		}
	}

	// Auch der alternative Compose-Dateiname wird gefunden.
	if !strings.HasSuffix(d.Stacks[1].ComposePath, "docker-compose.yml") {
		t.Errorf("alternativer dateiname nicht erkannt: %q", d.Stacks[1].ComposePath)
	}
}

func TestDiscoverStackFileOverrides(t *testing.T) {
	root := t.TempDir()
	write(t, root, "stacks/nas-01/media/compose.yaml", beispielCompose)
	write(t, root, "stacks/nas-01/media/stack.yaml",
		"hosts: [nas-01, nas-02]\nmode: apply\nupdates: auto\nhealth_window: 90s\n")

	d, err := gitsync.Discover(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Stacks) != 1 {
		t.Fatalf("%d stacks", len(d.Stacks))
	}
	s := d.Stacks[0]
	if len(s.Hosts) != 2 || s.Hosts[1] != "nas-02" {
		t.Errorf("Hosts = %v, stack.yaml hat nicht überschrieben", s.Hosts)
	}
	if s.Mode != gitsync.ModeApply {
		t.Errorf("Mode = %q, erwartet apply", s.Mode)
	}
	if s.Updates != gitsync.UpdateAuto {
		t.Errorf("Updates = %q", s.Updates)
	}
	if s.HealthWindow.Seconds() != 90 {
		t.Errorf("HealthWindow = %v", s.HealthWindow)
	}
}

// TestDiscoverCollectsProblems belegt: Ein kaputtes Verzeichnis verhindert
// nicht die Erkennung der übrigen. Der Nutzer sieht beides.
func TestDiscoverCollectsProblems(t *testing.T) {
	root := t.TempDir()
	write(t, root, "stacks/nas-01/gut/compose.yaml", beispielCompose)
	// Verzeichnis ohne Compose-Datei.
	if err := os.MkdirAll(filepath.Join(root, "stacks/nas-01/leer"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Unlesbare stack.yaml.
	write(t, root, "stacks/nas-01/kaputt/compose.yaml", beispielCompose)
	write(t, root, "stacks/nas-01/kaputt/stack.yaml", "hosts: [nas-01\n  mode: ]]]")
	// Unbekannter Modus.
	write(t, root, "stacks/nas-01/falschermodus/compose.yaml", beispielCompose)
	write(t, root, "stacks/nas-01/falschermodus/stack.yaml", "mode: zerstoere-alles\n")

	d, err := gitsync.Discover(root, "")
	if err != nil {
		t.Fatalf("Discover darf bei Einzelproblemen nicht scheitern: %v", err)
	}
	if len(d.Stacks) != 1 || d.Stacks[0].Name != "gut" {
		t.Fatalf("der intakte stack fehlt: %+v", d.Stacks)
	}
	if len(d.Problems) != 3 {
		t.Fatalf("%d probleme gemeldet, erwartet 3: %+v", len(d.Problems), d.Problems)
	}
	// Ein unbekannter Modus darf nicht stillschweigend zu apply werden.
	for _, p := range d.Problems {
		if strings.Contains(p.Path, "falschermodus") && !strings.Contains(p.Message, "unbekannt") {
			t.Errorf("meldung zum unbekannten modus unklar: %q", p.Message)
		}
	}
}

func TestDiscoverWithBasePath(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/homelab/stacks/nas-01/media/compose.yaml", beispielCompose)

	if _, err := gitsync.Discover(root, ""); err == nil {
		t.Error("ohne base_path dürfte nichts gefunden werden")
	}
	d, err := gitsync.Discover(root, "infra/homelab")
	if err != nil {
		t.Fatalf("mit base_path: %v", err)
	}
	if len(d.Stacks) != 1 {
		t.Fatalf("%d stacks", len(d.Stacks))
	}
}

// TestReadComposeRejectsEscape prüft, dass ein Pfad nicht aus der
// Arbeitskopie herausführen kann.
func TestReadComposeRejectsEscape(t *testing.T) {
	root := t.TempDir()
	write(t, root, "stacks/nas-01/media/compose.yaml", beispielCompose)

	if _, err := gitsync.ReadCompose(root, "stacks/nas-01/media/compose.yaml"); err != nil {
		t.Fatalf("gültiger pfad abgelehnt: %v", err)
	}
	for _, bad := range []string{"../../../etc/passwd", "/etc/passwd", "stacks/../../etc/hosts"} {
		if _, err := gitsync.ReadCompose(root, bad); err == nil {
			t.Errorf("pfad %q wurde nicht abgelehnt", bad)
		}
	}
}
