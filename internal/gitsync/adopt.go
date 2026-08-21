package gitsync

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetServiceImage ersetzt den Image-Wert eines Dienstes in einer Compose-Datei.
//
// Bewusst zeilenbasiert und punktgenau statt über ein Neuschreiben der Datei
// (ADR-0028): Ein Werkzeug, das eine Compose-Datei neu erzeugt und dabei
// Kommentare, Reihenfolge und Formatierung verliert, wird zu Recht nie wieder
// benutzt.
//
// Geändert wird ausschließlich der Skalarwert hinter `image:` innerhalb des
// betroffenen Dienstes. Alles andere — Einrückung, Kommentare am Zeilenende,
// Anführungszeichen — bleibt unberührt.
func SetServiceImage(content []byte, service, newImage string) ([]byte, error) {
	lines := splitLines(content)

	servicesIndent := -1
	serviceIndent := -1
	inServices := false
	inTarget := false
	changed := false

	var out bytes.Buffer
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		switch {
		case !inServices && trimmed == "services:":
			inServices = true
			servicesIndent = indent

		case inServices && trimmed != "" && !strings.HasPrefix(trimmed, "#"):
			// Ein Block auf oder unter der Ebene von `services:` beendet ihn.
			if indent <= servicesIndent {
				inServices, inTarget = false, false
				break
			}
			// Die erste Ebene darunter sind die Dienstnamen.
			if serviceIndent == -1 || indent == serviceIndent {
				if strings.HasSuffix(trimmed, ":") || strings.Contains(trimmed, ": ") {
					name := strings.TrimSuffix(strings.SplitN(trimmed, ":", 2)[0], ":")
					if serviceIndent == -1 {
						serviceIndent = indent
					}
					inTarget = strings.TrimSpace(name) == service
				}
				break
			}
			// Innerhalb des gesuchten Dienstes die image-Zeile finden.
			if inTarget && indent > serviceIndent && strings.HasPrefix(trimmed, "image:") {
				rest := strings.TrimPrefix(trimmed, "image:")
				comment := ""
				// Ein Kommentar am Zeilenende bleibt erhalten — samt seines
				// Abstands. Die Ausrichtung mehrerer Kommentare untereinander
				// ist Absicht des Autors, keine zufällige Formatierung.
				if idx := indexUnquoted(rest, '#'); idx >= 0 {
					comment = rest[idx:]
					// Abstand zwischen Wert und Kommentar unverändert lassen.
					vorher := rest[:idx]
					abstand := vorher[len(strings.TrimRight(vorher, " \t")):]
					comment = abstand + comment
				}
				prefix := line[:indent]
				out.WriteString(prefix + "image: " + newImage + comment + "\n")
				changed = true
				continue
			}
		}
		_ = i
		out.WriteString(line + "\n")
	}

	if !changed {
		return nil, fmt.Errorf("keine image-zeile für dienst %q gefunden", service)
	}
	return out.Bytes(), nil
}

// indexUnquoted findet ein Zeichen außerhalb von Anführungszeichen.
func indexUnquoted(s string, target rune) int {
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == target:
			return i
		}
	}
	return -1
}

func splitLines(b []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// EditAndPush ändert eine Datei, committet und pusht — alles unter derselben
// Sperre wie die Synchronisation.
//
// Die Sperre über den gesamten Vorgang ist wesentlich: Zwischen dem Schreiben
// der Datei und dem Commit darf kein `reset --hard` der Synchronisationsschleife
// dazwischenkommen. Sonst verschwände die Änderung spurlos, und der Nutzer
// sähe einen Erfolg, der keiner war.
//
// edit bekommt den aktuellen Dateiinhalt und liefert den neuen. Gibt es
// nichts zu ändern, liefert edit einen Fehler und es wird nichts geschrieben.
func (s *Syncer) EditAndPush(
	ctx context.Context,
	relPath, message, author string,
	edit func(current []byte) ([]byte, error),
) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	abs, err := s.absPathLocked(relPath)
	if err != nil {
		return "", err
	}
	current, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("datei lesen: %w", err)
	}
	updated, err := edit(current)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, updated, 0o644); err != nil {
		return "", fmt.Errorf("datei schreiben: %w", err)
	}

	commit, err := s.commitAndPushLocked(ctx, relPath, message, author)
	if err != nil {
		// Arbeitskopie wieder auf den Stand des Repos bringen — die
		// gescheiterte Änderung darf nicht liegen bleiben (ADR-0002).
		_, _ = s.run(ctx, "checkout", "--", relPath)
		return "", err
	}
	return commit, nil
}

// commitAndPushLocked erwartet eine bereits gehaltene Sperre.
func (s *Syncer) commitAndPushLocked(ctx context.Context, relPath, message, author string) (string, error) {
	before, err := s.headCommit(ctx)
	if err != nil {
		return "", err
	}

	if _, err := s.run(ctx, "add", "--", relPath); err != nil {
		return "", err
	}

	// Autor als Trailer statt als Git-Autor: Die Nutzer der Plattform haben
	// keine Git-Identität, und eine erfundene wäre irreführend.
	msg := message
	if author != "" {
		msg += "\n\nÜbernommen über die Homelab Platform von: " + author
	}
	if _, err := s.run(ctx, "-c", "user.name=homelab-platform",
		"-c", "user.email=homelab-platform@localhost",
		"commit", "-m", msg); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	after, err := s.headCommit(ctx)
	if err != nil {
		return "", err
	}

	if _, err := s.run(ctx, "push", "origin", "HEAD:"+s.cfg.Branch); err != nil {
		// Zurück auf den Stand vor dem Commit.
		if _, rerr := s.run(ctx, "reset", "--hard", before); rerr != nil {
			return "", fmt.Errorf("push fehlgeschlagen (%w) und rücksetzen misslang: %v", err, rerr)
		}
		return "", fmt.Errorf("push fehlgeschlagen — hat der deploy-key schreibrechte? %w", err)
	}
	return after, nil
}

// CanPush prüft, ob Schreibzugriff besteht.
//
// Wird für die Anzeige verwendet: Ohne Schreibrecht bleibt adopt deaktiviert,
// statt erst beim Klick zu scheitern (ADR-0028).
//
// ACHTUNG: Das ist ein echter Netzwerkaufruf. Der Aufrufer muss das Ergebnis
// zwischenspeichern — siehe repoManager.CanPush.
func (s *Syncer) CanPush(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// --dry-run verändert nichts, prüft aber die Berechtigung wirklich.
	_, err := s.run(ctx, "push", "--dry-run", "origin", "HEAD:"+s.cfg.Branch)
	return err == nil
}

// WorkDir liefert das Verzeichnis der Arbeitskopie.
func (s *Syncer) WorkDir() string { return s.cfg.WorkDir }

// absPathLocked baut einen geprüften Pfad in der Arbeitskopie.
//
// Ein Pfad aus dem Repo darf niemals aus der Arbeitskopie herausführen.
func (s *Syncer) absPathLocked(rel string) (string, error) {
	clean := filepath.Clean(filepath.Join(s.cfg.WorkDir, rel))
	root := filepath.Clean(s.cfg.WorkDir)
	if !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("pfad %q liegt außerhalb der arbeitskopie", rel)
	}
	return clean, nil
}
