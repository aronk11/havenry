package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ComposeTimeout begrenzt einen Compose-Aufruf.
//
// Großzügig bemessen: Ein `up` kann Images ziehen, und im Homelab hängt das an
// einer Haushaltsleitung. Zu knapp wäre schlimmer als zu großzügig — ein
// abgebrochener Pull hinterlässt einen halb aktualisierten Stack.
const ComposeTimeout = 15 * time.Minute

// Compose führt `docker compose` aus (ADR-0027).
//
// Compose-Semantik selbst nachzubauen hieße, Abhängigkeitsreihenfolge,
// Netzwerke, Volumes, Namensgebung und Merge-Regeln nachzuimplementieren und
// bei jeder Version nachzuziehen. Das Werkzeug, das der Nutzer ohnehin kennt,
// verhält sich garantiert richtig.
type Compose struct {
	// StackDir ist das Verzeichnis, in dem Stack-Dateien abgelegt werden.
	StackDir string
	// DockerSocket wird an die CLI durchgereicht, damit sie denselben Daemon
	// anspricht wie der Provider.
	DockerSocket string
	Timeout      time.Duration
}

func NewCompose(stackDir, socket string) *Compose {
	return &Compose{StackDir: stackDir, DockerSocket: socket, Timeout: ComposeTimeout}
}

// Available prüft, ob `docker compose` vorhanden ist.
//
// Wird beim Agent-Start aufgerufen: Fehlt die CLI, meldet der Agent die
// Fähigkeit `apply` gar nicht erst, statt Kommandos später unerklärt scheitern
// zu lassen.
func (c *Compose) Available(ctx context.Context) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "compose", "version", "--short").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// stackPath baut den Ablagepfad eines Stacks und prüft ihn.
//
// Der Stack-Name stammt aus dem Repo, also aus fremder Hand. Ohne Prüfung
// könnte ein Name wie "../../etc" den Agenten dazu bringen, außerhalb seines
// Verzeichnisses zu schreiben.
func (c *Compose) stackPath(stack string) (string, error) {
	if stack == "" {
		return "", fmt.Errorf("stack-name ist leer")
	}
	if strings.ContainsAny(stack, `/\`) || strings.Contains(stack, "..") {
		return "", fmt.Errorf("stack-name %q enthält unzulässige zeichen", stack)
	}
	dir := filepath.Join(c.StackDir, stack)

	clean := filepath.Clean(dir)
	root := filepath.Clean(c.StackDir)
	if !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("pfad für stack %q liegt außerhalb des arbeitsverzeichnisses", stack)
	}
	return clean, nil
}

// WriteStack legt die Compose-Datei ab.
//
// Vorhandene .env-Dateien im selben Verzeichnis bleiben unangetastet und werden
// von Compose weiterverwendet — die Plattform verwaltet keine Secrets (ADR-0006).
func (c *Compose) WriteStack(stack string, composeYAML []byte) (string, error) {
	dir, err := c.stackPath(stack)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("stack-verzeichnis anlegen: %w", err)
	}

	path := filepath.Join(dir, "compose.yaml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, composeYAML, 0o600); err != nil {
		return "", fmt.Errorf("compose-datei schreiben: %w", err)
	}
	// Atomar ersetzen: Ein abgebrochener Schreibvorgang darf keine halbe
	// Compose-Datei hinterlassen, die dann ausgeführt würde.
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("compose-datei ersetzen: %w", err)
	}
	return path, nil
}

// Result ist das Ergebnis eines Compose-Aufrufs.
type Result struct {
	Output string
	// Changed meldet, ob Compose tatsächlich etwas verändert hat.
	Changed bool
}

// Up bringt einen Stack in den beschriebenen Zustand.
//
// `--remove-orphans` entfernt Container, die zum Projekt gehören, aber nicht
// mehr in der Datei stehen — genau das erwartet man von einem Abgleich mit dem
// Soll-Zustand.
func (c *Compose) Up(ctx context.Context, stack string, composeYAML []byte) (Result, error) {
	path, err := c.WriteStack(stack, composeYAML)
	if err != nil {
		return Result{}, err
	}
	out, err := c.run(ctx, stack, path, "up", "-d", "--remove-orphans")
	if err != nil {
		return Result{Output: out}, err
	}
	return Result{Output: out, Changed: composeChangedSomething(out)}, nil
}

// Pull holt die Images eines Stacks.
func (c *Compose) Pull(ctx context.Context, stack string, composeYAML []byte) (Result, error) {
	path, err := c.WriteStack(stack, composeYAML)
	if err != nil {
		return Result{}, err
	}
	out, err := c.run(ctx, stack, path, "pull")
	return Result{Output: out, Changed: true}, err
}

// Down hält einen Stack an und entfernt seine Container.
//
// Volumes bleiben absichtlich erhalten: `down -v` würde Daten löschen. Das
// darf kein Kommando tun, das aus einer Weboberfläche ausgelöst wird.
func (c *Compose) Down(ctx context.Context, stack string) (Result, error) {
	dir, err := c.stackPath(stack)
	if err != nil {
		return Result{}, err
	}
	path := filepath.Join(dir, "compose.yaml")
	if _, err := os.Stat(path); err != nil {
		return Result{}, fmt.Errorf("stack %q ist auf diesem host nicht bekannt", stack)
	}
	out, err := c.run(ctx, stack, path, "down")
	return Result{Output: out, Changed: true}, err
}

// composeChangedSomething liest an der Ausgabe ab, ob etwas passiert ist.
//
// Compose meldet unveränderte Dienste als "Running" und geänderte als
// "Started"/"Recreated". Das ist eine Heuristik über Ausgabetext und deshalb
// nur für die Anzeige gedacht — nie für eine Entscheidung.
func composeChangedSomething(out string) bool {
	for _, marker := range []string{"Started", "Recreated", "Created", "Removed", "Stopped"} {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}

func (c *Compose) run(ctx context.Context, stack, file string, args ...string) (string, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = ComposeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := append([]string{"compose", "-p", stack, "-f", file}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = filepath.Dir(file)

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		// Ohne das fragt Compose bei manchen Aktionen nach.
		"COMPOSE_INTERACTIVE_NO_CLI=1",
		"DOCKER_CLI_HINTS=false",
		"LC_ALL=C",
	}
	if c.DockerSocket != "" && !strings.HasPrefix(c.DockerSocket, "tcp://") {
		env = append(env, "DOCKER_HOST=unix://"+c.DockerSocket)
	} else if c.DockerSocket != "" {
		env = append(env, "DOCKER_HOST="+c.DockerSocket)
	}
	cmd.Env = env

	var buf bytes.Buffer
	// Compose schreibt seinen Fortschritt auf stderr. Beides zusammen ist das,
	// was der Nutzer auf der Kommandozeile sähe.
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := strings.TrimSpace(buf.String())

	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("docker compose %s: zeitüberschreitung nach %s", args[0], timeout)
		}
		// Die Ausgabe von Compose wird vollständig durchgereicht — sie ist für
		// diese Zielgruppe aussagekräftiger als jede Umschreibung.
		return out, fmt.Errorf("docker compose %s fehlgeschlagen: %s", args[0], out)
	}
	return out, nil
}
