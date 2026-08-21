package updates_test

import (
	"strings"
	"testing"

	"github.com/aronk11/havenry/internal/updates"
)

// Diese Einordnung entscheidet, ob jemand vor einem Update gewarnt wird.
// Zu selten warnen heißt: Es bricht, und Havenry hat geschwiegen. Zu oft
// warnen heißt: Man klickt es weg, und dann bricht es trotzdem.

func TestCompareSeverity(t *testing.T) {
	cases := []struct {
		cur, next string
		want      updates.Severity
		why       string
	}{
		{"1.2.3", "1.2.4", updates.SeverityPatch, "letzte Stelle"},
		{"1.2.3", "1.3.0", updates.SeverityMinor, "Nebenversion"},
		{"1.2.3", "2.0.0", updates.SeverityMajor, "Hauptversion"},
		{"10.9.2", "10.10.0", updates.SeverityMinor, "zweistellige Nebenversion, nicht alphabetisch vergleichen"},
		{"v1.2.3", "v1.2.4", updates.SeverityPatch, "führendes v"},
		{"1.2", "1.3", updates.SeverityMinor, "ohne Patchstelle"},

		// Vor 1.0 gilt laut Semver jede Nebenversion als potenziell brechend —
		// und in der Praxis ist sie es auch.
		{"0.4.2", "0.5.0", updates.SeverityMajor, "vor 1.0 ist minor brechend"},
		{"0.4.2", "0.4.3", updates.SeverityPatch, "vor 1.0 bleibt patch harmlos"},

		// Bewegliche Tags tragen keine Ordnung.
		{"latest", "latest", updates.SeverityUnknown, "latest ist keine Version"},
		{"1.2.3", "latest", updates.SeverityUnknown, "Ziel unvergleichbar"},
		{"stable", "1.2.3", updates.SeverityUnknown, "Quelle unvergleichbar"},
		{"2024-01-15", "2024-02-01", updates.SeverityUnknown, "Datumsstände sehen aus wie Versionen"},
		{"20240115", "20250101", updates.SeverityUnknown, "Datumsstand ohne Trenner"},
		// Kalenderversionierung (2023.10.1) wird bewusst wie Semver gelesen.
		// Ein Jahreswechsel erscheint dadurch als Hauptversion — das warnt
		// jeden Januar zu viel. Das ist die richtige Richtung des Fehlers:
		// zu oft warnen kostet Aufmerksamkeit, zu selten warnen kostet den
		// Stack.
		{"2023.10.1", "2023.11.1", updates.SeverityMinor, "CalVer innerhalb eines Jahres"},
		{"abc1234", "def5678", updates.SeverityUnknown, "Git-Kürzel"},

		// Ein Rückschritt ist Absicht, kein Update.
		{"1.5.0", "1.4.0", updates.SeverityUnknown, "Rückschritt"},
		{"2.0.0", "1.9.9", updates.SeverityUnknown, "Hauptversion zurück"},

		// Varianten sind untereinander nicht vergleichbar.
		{"1.2.3-alpine", "1.2.4-alpine", updates.SeverityPatch, "gleiche Variante"},
		{"1.2.3-alpine", "1.2.4-ubuntu", updates.SeverityUnknown, "Variante gewechselt"},
	}

	for _, c := range cases {
		got := updates.Compare(c.cur, c.next)
		if got != c.want {
			t.Errorf("Compare(%q, %q) = %q, erwartet %q — %s",
				c.cur, c.next, got, c.want, c.why)
		}
	}
}

// TestTenDotTenIsNotBelowTenDotNine ist der Fall, an dem ein naiver
// Zeichenvergleich scheitert: "10.10" < "10.9" alphabetisch.
func TestTenDotTenIsNotBelowTenDotNine(t *testing.T) {
	if got := updates.Compare("10.9.0", "10.10.0"); got != updates.SeverityMinor {
		t.Fatalf("10.9.0 → 10.10.0 = %q, erwartet minor — wurde alphabetisch verglichen?", got)
	}
	if got := updates.Compare("10.10.0", "10.9.0"); got != updates.SeverityUnknown {
		t.Fatalf("Rückschritt 10.10 → 10.9 = %q, erwartet unknown", got)
	}
}

func TestParseVersion(t *testing.T) {
	// Bewegliche Tags und Datumsstände tragen keine vergleichbare Ordnung.
	for _, tag := range []string{
		"latest", "stable", "main", "edge", "nightly", "",
		"2024-01-15", "20240115",
	} {
		if _, ok := updates.ParseVersion(tag); ok {
			t.Errorf("%q wurde als Version gelesen — daraus lässt sich kein Sprung ableiten", tag)
		}
	}

	v, ok := updates.ParseVersion("v10.9.2-alpine")
	if !ok {
		t.Fatal("v10.9.2-alpine wurde nicht gelesen")
	}
	if v.Major != 10 || v.Minor != 9 || v.Patch != 2 {
		t.Fatalf("gelesen als %d.%d.%d", v.Major, v.Minor, v.Patch)
	}
	if v.Suffix != "-alpine" {
		t.Errorf("Suffix = %q", v.Suffix)
	}
}

// --- Release Notes -------------------------------------------------------

func TestFindBreakingCatchesCommonPhrasings(t *testing.T) {
	notes := `
## 2.0.0

### Features
- Neue Oberfläche
- Schnellere Indizierung

### BREAKING CHANGES
- Die Konfigurationsdatei heißt jetzt config.yaml statt settings.yaml
- Support für Python 3.8 wurde entfernt

### Fixes
- Absturz beim Start behoben
`
	found := updates.FindBreaking(notes)
	if len(found) == 0 {
		t.Fatal("keine brechende Änderung erkannt, obwohl BREAKING CHANGES dasteht")
	}

	all := strings.Join(linesOf(found), "\n")
	if !strings.Contains(all, "BREAKING CHANGES") {
		t.Errorf("die Überschrift fehlt im Fund:\n%s", all)
	}
}

func TestFindBreakingVariousMarkers(t *testing.T) {
	cases := []struct {
		note string
		want bool
		why  string
	}{
		{"⚠️ Die Datenbank muss migriert werden", true, "Warnzeichen"},
		{"Migration required before upgrading", true, "migration required"},
		{"This release is incompatible with 1.x", true, "incompatible"},
		{"The old API endpoint is no longer supported", true, "no longer supported"},
		{"Action required: rotate your keys", true, "action required"},
		{"You must update your compose file", true, "you must update"},
		{"Dropped support for ARMv6", true, "dropped support"},

		{"Fixed a crash on startup", false, "harmlos"},
		{"Improved performance by 20%", false, "harmlos"},
		{"Added a new setting for cache size", false, "harmlos"},
		{"", false, "leer"},
	}

	for _, c := range cases {
		got := len(updates.FindBreaking(c.note)) > 0
		if got != c.want {
			t.Errorf("FindBreaking(%q) = %v, erwartet %v — %s", c.note, got, c.want, c.why)
		}
	}
}

// TestFindBreakingKeepsOriginalWording: Bei genau dieser Frage will niemand
// eine Zusammenfassung, sondern den Wortlaut.
func TestFindBreakingKeepsOriginalWording(t *testing.T) {
	line := "BREAKING: config.yaml ersetzt settings.yaml — vor dem Update umbenennen"
	found := updates.FindBreaking(line)
	if len(found) != 1 {
		t.Fatalf("%d Funde, erwartet 1", len(found))
	}
	if found[0].Line != line {
		t.Errorf("Wortlaut verändert:\n  gefunden: %s\n  original: %s", found[0].Line, line)
	}
	if found[0].Marker == "" {
		t.Error("kein Marker vermerkt — dann ist nicht nachvollziehbar, warum die Zeile auffiel")
	}
}

func TestFindBreakingDeduplicates(t *testing.T) {
	notes := strings.Repeat("BREAKING: dieselbe Zeile\n", 5)
	if got := len(updates.FindBreaking(notes)); got != 1 {
		t.Fatalf("%d Funde für fünf gleiche Zeilen, erwartet 1", got)
	}
}

func TestSplitImage(t *testing.T) {
	cases := []struct{ in, name, tag string }{
		{"nginx:1.27", "nginx", "1.27"},
		{"nginx", "nginx", "latest"},
		{"ghcr.io/foo/bar:v2", "ghcr.io/foo/bar", "v2"},
		{"registry.local:5000/app:1.0", "registry.local:5000/app", "1.0"},
		{"registry.local:5000/app", "registry.local:5000/app", "latest"},
		{"nginx@sha256:abc", "nginx", ""},
	}
	for _, c := range cases {
		name, tag := updates.SplitImage(c.in)
		if name != c.name || tag != c.tag {
			t.Errorf("SplitImage(%q) = (%q, %q), erwartet (%q, %q)",
				c.in, name, tag, c.name, c.tag)
		}
	}
}

func linesOf(ws []updates.Warning) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Line)
	}
	return out
}
