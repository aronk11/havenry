// Package updates ordnet verfügbare Image-Aktualisierungen ein.
//
// Der Mehrwert liegt nicht im Update selbst — `docker compose pull` kann das
// auch. Er liegt darin, dass vor dem Klick steht, was man sich einhandelt
// (ADR-0033).
package updates

import (
	"regexp"
	"strconv"
	"strings"
)

// Severity beschreibt, wie groß ein Versionssprung ist.
type Severity string

const (
	// SeverityPatch: nur die letzte Stelle. In aller Regel gefahrlos.
	SeverityPatch Severity = "patch"
	// SeverityMinor: neue Funktionen, laut Semver rückwärtskompatibel.
	SeverityMinor Severity = "minor"
	// SeverityMajor: der Fall, bei dem man vorher lesen sollte.
	SeverityMajor Severity = "major"
	// SeverityUnknown: Tags, die sich nicht vergleichen lassen — `latest`,
	// Datumsstände, Git-Kürzel. Ehrlicher als eine geratene Einordnung.
	SeverityUnknown Severity = "unknown"
)

// Version ist ein aufgelöster Versionstag.
type Version struct {
	Major, Minor, Patch int
	// Suffix hält Vorabkennzeichnungen wie "-rc1" fest.
	Suffix string
	// Raw ist der ursprüngliche Tag.
	Raw string
}

// semverish erkennt die üblichen Schreibweisen: 1.2.3, v1.2.3, 1.2, 10.9.
//
// Bewusst großzügig: Container-Tags folgen selten sauberem Semver. Ein
// Erkenner, der nur exaktes Semver akzeptiert, würde bei den meisten
// Homelab-Images passen.
var semverish = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(.*)$`)

// ParseVersion liest einen Tag. ok ist false, wenn er sich nicht vergleichen
// lässt.
func ParseVersion(tag string) (Version, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return Version{}, false
	}

	m := semverish.FindStringSubmatch(tag)
	if m == nil {
		// Deckt bereits alle Tags ohne führende Ziffer ab: latest, stable,
		// main, edge, nightly. Eine zusätzliche Namensliste wäre toter Code —
		// sie wurde entfernt, nachdem ein Mutationstest zeigte, dass sie nie
		// erreicht wird.
		return Version{Raw: tag}, false
	}

	// Datumsstände mit Trennern (2024-01-15, 20240115) sehen aus wie
	// Versionen und sind keine. Ohne diese Prüfung wäre der Sprung auf den
	// nächsten Tag "eine Hauptversion".
	//
	// Bewusst nicht abgefangen: Kalenderversionierung wie 2023.10.1. Sie ist
	// tatsächlich geordnet und wird wie Semver gelesen. Ein Jahreswechsel
	// erscheint dadurch als Hauptversion — also einmal jährlich eine Warnung
	// zu viel. Das ist die richtige Richtung des Fehlers: Zu oft warnen kostet
	// Aufmerksamkeit, zu selten warnen kostet den Stack.
	if isDateLike(tag) {
		return Version{Raw: tag}, false
	}

	v := Version{Raw: tag, Suffix: m[4]}
	v.Major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		v.Minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}

	// Ein Suffix, das nicht nach Vorabversion aussieht (etwa "-ubuntu" oder
	// "-alpine"), gehört zur Variante und nicht zur Version. Solche Tags sind
	// untereinander vergleichbar, solange die Variante dieselbe bleibt.
	return v, true
}

// Compare ordnet den Sprung von cur nach next ein.
//
// Ein Rückschritt gilt als unbekannt statt als Update: Wer von 1.5 auf 1.4
// wechselt, tut das absichtlich, und eine Einordnung als "Patch" wäre falsch.
func Compare(cur, next string) Severity {
	a, okA := ParseVersion(cur)
	b, okB := ParseVersion(next)
	if !okA || !okB {
		return SeverityUnknown
	}

	// Unterschiedliche Varianten (alpine vs. ubuntu) sind nicht vergleichbar.
	if variant(a.Suffix) != variant(b.Suffix) {
		return SeverityUnknown
	}

	switch {
	case b.Major > a.Major:
		return SeverityMajor
	case b.Major < a.Major:
		return SeverityUnknown
	case b.Minor > a.Minor:
		// Vor 1.0 ist laut Semver jede Nebenversion potenziell brechend.
		// Das steht so in der Spezifikation und wird in der Praxis auch so
		// gehandhabt.
		if a.Major == 0 {
			return SeverityMajor
		}
		return SeverityMinor
	case b.Minor < a.Minor:
		return SeverityUnknown
	case b.Patch > a.Patch:
		return SeverityPatch
	default:
		return SeverityUnknown
	}
}

// dateLike erkennt Tags wie 2024-01-15 oder 20240115.
var dateLike = regexp.MustCompile(`^v?(19|20)\d{2}([.\-_]?\d{2}){2}`)

func isDateLike(tag string) bool { return dateLike.MatchString(tag) }

// variant trennt Vorabkennzeichnungen von Varianten-Suffixen.
func variant(suffix string) string {
	s := strings.ToLower(strings.TrimPrefix(suffix, "-"))
	for _, pre := range []string{"rc", "beta", "alpha", "pre", "dev", "snapshot"} {
		if strings.HasPrefix(s, pre) {
			return ""
		}
	}
	return s
}

// breakingMarkers sind die Formulierungen, an denen sich brechende Änderungen
// in Release Notes erkennen lassen.
//
// Das ist eine Textsuche in fremdem Text, keine Garantie — und die Oberfläche
// sagt das auch. Sie behauptet nie "keine brechenden Änderungen", sondern
// höchstens "nichts gefunden" (ADR-0033).
var breakingMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bbreaking[ -]?change`),
	regexp.MustCompile(`(?i)\bbreaking\b`),
	regexp.MustCompile(`(?i)\bmigration (required|needed|guide)`),
	regexp.MustCompile(`(?i)\bincompatible\b`),
	regexp.MustCompile(`(?i)\bno longer (supported|works|available)`),
	regexp.MustCompile(`(?i)\b(removed|dropped) support\b`),
	regexp.MustCompile(`(?i)\byou (must|need to) (update|change|migrate)`),
	regexp.MustCompile(`(?i)\baction required\b`),
	regexp.MustCompile(`(?i)^#+.*\bupgrad(e|ing) (notes|guide)`),
	regexp.MustCompile(`⚠️|:warning:`),
}

// Warning ist eine gefundene Stelle in den Release Notes.
type Warning struct {
	// Line ist die Fundstelle im Originalwortlaut, gekürzt.
	Line string `json:"line"`
	// Marker nennt, worauf sie angesprungen ist — damit nachvollziehbar
	// bleibt, warum etwas hervorgehoben wurde.
	Marker string `json:"marker"`
}

// FindBreaking durchsucht Release Notes nach Hinweisen auf brechende
// Änderungen.
//
// Es wird zeilenweise gesucht und die Fundstelle unverändert zurückgegeben.
// Eine Zusammenfassung wäre eine Deutung — und bei genau dieser Frage will
// niemand eine Deutung, sondern den Wortlaut.
func FindBreaking(notes string) []Warning {
	var out []Warning
	seen := map[string]bool{}

	for _, raw := range strings.Split(notes, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		for _, re := range breakingMarkers {
			if !re.MatchString(line) {
				continue
			}
			short := line
			if len(short) > 240 {
				short = short[:237] + "…"
			}
			if seen[short] {
				break
			}
			seen[short] = true
			out = append(out, Warning{Line: short, Marker: re.String()})
			break
		}
	}
	return out
}

// SplitImage zerlegt einen Image-Bezeichner in Name und Tag.
func SplitImage(image string) (name, tag string) {
	if i := strings.Index(image, "@"); i > 0 {
		// Digest-Angabe trägt keinen lesbaren Tag.
		return image[:i], ""
	}
	lastSlash := strings.LastIndex(image, "/")
	if i := strings.LastIndex(image, ":"); i > lastSlash {
		return image[:i], image[i+1:]
	}
	return image, "latest"
}
