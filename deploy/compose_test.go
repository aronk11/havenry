package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Die Compose-Beispiele sind das Erste, was ein neuer Nutzer anfasst. Ein
// Fehler darin kostet ihn den ersten Eindruck — und wird sonst erst gemeldet,
// wenn jemand es tatsächlich versucht hat.
//
// Genau so ist der Fehler entstanden, den dieser Test verhindert: Der
// Schnellstart verlangte ein Enrollment-Token, bevor die Control Plane lief,
// die es hätte ausstellen können.

// requiredVar findet ${VAR:?...} — Variablen, ohne die Compose die Datei
// nicht einmal einliest.
var requiredVar = regexp.MustCompile(`\$\{([A-Z_]+):\?`)

// TestControlPlaneStartsWithoutVariables hält den gemeldeten Fehler fest.
//
// Compose löst ALLE Variablen einer Datei beim Einlesen auf, nicht erst beim
// Starten eines Dienstes. Eine Pflichtvariable irgendwo in compose.yaml
// blockiert deshalb auch den Start der Control Plane — und die ist genau das,
// was man zuerst braucht, um an den Wert zu kommen.
func TestControlPlaneStartsWithoutVariables(t *testing.T) {
	data, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatalf("compose.yaml lesen: %v", err)
	}

	if found := requiredVar.FindAllStringSubmatch(string(data), -1); len(found) > 0 {
		var names []string
		for _, m := range found {
			names = append(names, m[1])
		}
		t.Fatalf("compose.yaml verlangt %v — damit scheitert schon der erste Befehl.\n"+
			"Alles, was ein Token oder eine Adresse braucht, gehört in agent.yaml.",
			names)
	}
}

// TestAgentComposeAsksOnlyForWhatItNeeds: Der Agent darf fragen — aber nur
// nach dem, was ohne ihn nicht zu erraten ist.
func TestAgentComposeAsksOnlyForWhatItNeeds(t *testing.T) {
	data, err := os.ReadFile("agent.yaml")
	if err != nil {
		t.Fatalf("agent.yaml lesen: %v", err)
	}

	required := map[string]bool{}
	for _, m := range requiredVar.FindAllStringSubmatch(string(data), -1) {
		required[m[1]] = true
	}

	// Die Serveradresse kann niemand raten.
	if !required["HAVENRY_SERVER"] {
		t.Error("agent.yaml sollte HAVENRY_SERVER verlangen — die Adresse ist nicht zu erraten")
	}

	// Das Token darf NICHT verlangt werden: Beim zweiten Start hat der Agent
	// ein dauerhaftes Credential und braucht keins mehr. Ein Pflichtfeld
	// zwänge zu einer Attrappe bei jedem Neustart.
	if required["ENROLL_TOKEN"] {
		t.Error("ENROLL_TOKEN darf keine Pflichtvariable sein — nach dem ersten " +
			"Start hat der Agent ein Credential und braucht kein Token mehr")
	}
	if !strings.Contains(string(data), "ENROLL_TOKEN") {
		t.Error("agent.yaml sollte ENROLL_TOKEN kennen, nur eben optional")
	}
}

// TestReadmeQuickstartMatchesFiles: Ein Schnellstart, der auf eine Datei
// verweist, die es nicht gibt, ist schlimmer als keiner.
func TestReadmeQuickstartMatchesFiles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatalf("README lesen: %v", err)
	}
	readme := string(data)

	for _, f := range []string{"deploy/compose.yaml", "deploy/agent.yaml"} {
		if !strings.Contains(readme, f) {
			t.Errorf("README erwähnt %s nicht", f)
		}
		if _, err := os.Stat(filepath.Base(f)); err != nil {
			t.Errorf("README verweist auf %s, die Datei fehlt aber", f)
		}
	}

	// Die alte, zusammengelegte Datei darf nicht mehr vorkommen.
	if strings.Contains(readme, "agent-only.yaml") {
		t.Error("README verweist noch auf agent-only.yaml")
	}

	// Die Reihenfolge im Schnellstart muss stimmen: erst starten, dann Token.
	cp := strings.Index(readme, "deploy/compose.yaml")
	ag := strings.Index(readme, "deploy/agent.yaml")
	if cp < 0 || ag < 0 || cp > ag {
		t.Error("im Schnellstart steht der Agent vor der Control Plane — " +
			"ohne laufende Control Plane gibt es kein Token")
	}
}
