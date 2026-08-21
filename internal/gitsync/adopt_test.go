package gitsync_test

import (
	"strings"
	"testing"

	"github.com/aronk11/havenry/internal/gitsync"
)

// Diese Tests prüfen vor allem, was NICHT passiert: Kommentare, Formatierung
// und alle anderen Dienste müssen unangetastet bleiben.
//
// Ein Werkzeug, das eine Compose-Datei anfasst und dabei Kommentare verliert,
// wird zu Recht nie wieder benutzt (ADR-0028).

const composeMitKommentaren = `# Mein Medienstack
# Läuft auf dem NAS im Keller

services:

  jellyfin:
    # Version bewusst gepinnt, 10.10 macht Probleme mit der Hardware-Transkodierung
    image: jellyfin/jellyfin:10.9
    restart: unless-stopped
    ports:
      - "8096:8096"   # Weboberfläche
    volumes:
      - ./config:/config

  sonarr:
    image: linuxserver/sonarr:4.0    # nicht anfassen
    restart: unless-stopped

# Ende
`

func TestSetServiceImagePreservesEverythingElse(t *testing.T) {
	out, err := gitsync.SetServiceImage([]byte(composeMitKommentaren), "jellyfin", "jellyfin/jellyfin:10.10")
	if err != nil {
		t.Fatalf("SetServiceImage: %v", err)
	}
	result := string(out)

	if !strings.Contains(result, "image: jellyfin/jellyfin:10.10") {
		t.Fatal("neues image steht nicht in der datei")
	}
	if strings.Contains(result, "jellyfin/jellyfin:10.9") {
		t.Fatal("altes image ist noch da")
	}

	// Alle Kommentare müssen erhalten bleiben — auch der über der geänderten Zeile.
	for _, kommentar := range []string{
		"# Mein Medienstack",
		"# Läuft auf dem NAS im Keller",
		"# Version bewusst gepinnt",
		"# Weboberfläche",
		"# nicht anfassen",
		"# Ende",
	} {
		if !strings.Contains(result, kommentar) {
			t.Errorf("kommentar %q ging verloren", kommentar)
		}
	}

	// Der andere Dienst bleibt unberührt.
	if !strings.Contains(result, "image: linuxserver/sonarr:4.0    # nicht anfassen") {
		t.Error("der zweite dienst wurde verändert")
	}

	// Struktur und Zeilenzahl bleiben gleich.
	if strings.Count(result, "\n") != strings.Count(composeMitKommentaren, "\n") {
		t.Errorf("zeilenzahl geändert: vorher %d, nachher %d",
			strings.Count(composeMitKommentaren, "\n"), strings.Count(result, "\n"))
	}
	for _, teil := range []string{"restart: unless-stopped", "- \"8096:8096\"", "- ./config:/config"} {
		if !strings.Contains(result, teil) {
			t.Errorf("zeile %q ging verloren", teil)
		}
	}
}

// TestSetServiceImageKeepsTrailingComment: Ein Kommentar hinter der
// image-Zeile ist oft die Begründung für die Version. Er muss bleiben.
func TestSetServiceImageKeepsTrailingComment(t *testing.T) {
	in := "services:\n  web:\n    image: nginx:1.25   # letzte version mit modul x\n"
	out, err := gitsync.SetServiceImage([]byte(in), "web", "nginx:1.27")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "nginx:1.27") {
		t.Fatal("image nicht ersetzt")
	}
	if !strings.Contains(got, "# letzte version mit modul x") {
		t.Fatalf("kommentar am zeilenende ging verloren: %q", got)
	}
	// Der Abstand ist Absicht des Autors (Ausrichtung), nicht Zufall.
	if !strings.Contains(got, "nginx:1.27   # letzte version") {
		t.Errorf("abstand vor dem kommentar wurde verändert: %q", got)
	}
}

// TestSetServiceImageIgnoresHashInImageName: Ein Doppelkreuz innerhalb von
// Anführungszeichen ist kein Kommentar.
func TestSetServiceImageIgnoresHashInQuotes(t *testing.T) {
	in := "services:\n  web:\n    image: \"nginx:1.25\"\n"
	out, err := gitsync.SetServiceImage([]byte(in), "web", "nginx:1.27")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "#") {
		t.Errorf("ein kommentar wurde erfunden: %q", string(out))
	}
}

func TestSetServiceImagePreservesIndentation(t *testing.T) {
	// Vier Leerzeichen Einrückung statt der üblichen zwei.
	in := "services:\n    web:\n        image: nginx:1.25\n        restart: always\n"
	out, err := gitsync.SetServiceImage([]byte(in), "web", "nginx:1.27")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "        image: nginx:1.27") {
		t.Errorf("einrückung ging verloren: %q", string(out))
	}
}

func TestSetServiceImageRejectsUnknownService(t *testing.T) {
	_, err := gitsync.SetServiceImage([]byte(composeMitKommentaren), "gibtesnicht", "x:1")
	if err == nil {
		t.Fatal("unbekannter dienst wurde angenommen")
	}
}

// TestSetServiceImageDoesNotTouchSimilarNames: Ein Dienst namens "web" darf
// nicht "web-proxy" treffen.
func TestSetServiceImageDoesNotTouchSimilarNames(t *testing.T) {
	in := `services:
  web:
    image: nginx:1.25
  web-proxy:
    image: caddy:2.7
`
	out, err := gitsync.SetServiceImage([]byte(in), "web", "nginx:1.27")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "image: nginx:1.27") {
		t.Error("zieldienst nicht geändert")
	}
	if !strings.Contains(got, "image: caddy:2.7") {
		t.Error("ähnlich benannter dienst wurde mitgeändert")
	}
}

// TestSetServiceImageStopsAtBlockEnd: Ein `volumes:`-Block auf oberster Ebene
// nach den Diensten darf nicht mehr als Dienst gelesen werden.
func TestSetServiceImageStopsAtBlockEnd(t *testing.T) {
	in := `services:
  web:
    image: nginx:1.25

volumes:
  daten:
`
	out, err := gitsync.SetServiceImage([]byte(in), "web", "nginx:1.27")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "volumes:\n  daten:") {
		t.Errorf("der volumes-block wurde beschädigt: %q", got)
	}
}
