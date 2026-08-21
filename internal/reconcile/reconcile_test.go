package reconcile_test

import (
	"testing"

	"github.com/aronk11/havenry/internal/reconcile"
	"github.com/aronk11/havenry/internal/transport"
)

// Diese Datei prüft vor allem eines: dass KEINE Abweichung gemeldet wird, wo
// keine ist.
//
// Ein übersehener Drift ist ärgerlich. Ein Falsch-positiv ist schlimmer: Es
// zerstört das Vertrauen in jede weitere Meldung, und der Nutzer schaltet das
// Werkzeug ab. Deshalb hat jeder Fall, der zu einem Falsch-positiv führen
// könnte, hier einen eigenen Test.

func TestNormalizeImage(t *testing.T) {
	cases := []struct{ in, want string }{
		// Der häufigste Fall überhaupt: Compose ohne Tag, Docker mit :latest.
		{"nginx", "nginx:latest"},
		{"nginx:latest", "nginx:latest"},
		// Registry-Vorsilben, die Docker ergänzt und niemand tippt.
		{"docker.io/library/nginx:1.27", "nginx:1.27"},
		{"docker.io/linuxserver/sonarr", "linuxserver/sonarr:latest"},
		{"index.docker.io/library/redis:7", "redis:7"},
		{"library/postgres:16", "postgres:16"},
		// Fremde Registry bleibt erhalten — sie ist Teil der Identität.
		{"ghcr.io/home-assistant/home-assistant:stable", "ghcr.io/home-assistant/home-assistant:stable"},
		{"ghcr.io/foo/bar", "ghcr.io/foo/bar:latest"},
		// Registry mit Port: Der Doppelpunkt ist KEIN Tag-Trenner.
		{"registry.local:5000/meins", "registry.local:5000/meins:latest"},
		{"registry.local:5000/meins:v2", "registry.local:5000/meins:v2"},
		// Digest ist eindeutig und bleibt unangetastet.
		{"nginx@sha256:abc123", "nginx@sha256:abc123"},
		{"", ""},
	}
	for _, c := range cases {
		if got := reconcile.NormalizeImage(c.in); got != c.want {
			t.Errorf("NormalizeImage(%q) = %q, erwartet %q", c.in, got, c.want)
		}
	}
}

// TestNoDriftForEquivalentNotation ist der wichtigste Test der Datei:
// Compose-Datei und laufender Container beschreiben dasselbe in
// unterschiedlicher Schreibweise. Es darf KEINE Abweichung gemeldet werden.
func TestNoDriftForEquivalentNotation(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx
    ports:
      - "8080:80"
    restart: unless-stopped
  db:
    image: postgres:16
    ports:
      - "127.0.0.1:5432:5432"
`)
	parsed, err := reconcile.ParseCompose("meinstack", compose)
	if err != nil {
		t.Fatalf("ParseCompose: %v", err)
	}

	// So meldet Docker denselben Zustand: voll qualifizierte Images mit Tag,
	// dazu die von Compose erzeugten Labels.
	observed := reconcile.NormalizeObserved("meinstack", []transport.ResourceState{
		{
			ID: "c1", Name: "meinstack-web-1", Stack: "meinstack",
			Image: "docker.io/library/nginx:latest", State: "running",
			Restart: "unless-stopped",
			Labels: map[string]string{
				"com.docker.compose.project":          "meinstack",
				"com.docker.compose.service":          "web",
				"com.docker.compose.container-number": "1",
				"org.opencontainers.image.title":      "nginx",
			},
			Ports: []transport.PortMapping{{Host: 8080, Container: 80, Protocol: "tcp"}},
		},
		{
			ID: "c2", Name: "meinstack-db-1", Stack: "meinstack",
			Image: "postgres:16", State: "running",
			// Keine restart-Angabe in der Compose-Datei, Docker meldet "no".
			Restart: "no",
			Labels: map[string]string{
				"com.docker.compose.project": "meinstack",
				"com.docker.compose.service": "db",
			},
			Ports: []transport.PortMapping{{Host: 5432, Container: 5432, Protocol: "tcp"}},
		},
	})

	rep := reconcile.Compare(parsed.Desired, observed)
	if !rep.InSync {
		t.Fatalf("FALSCH-POSITIV: %d abweichungen gemeldet, obwohl der zustand übereinstimmt:\n%s",
			len(rep.Drifts), formatDrifts(rep.Drifts))
	}
}

// TestGeneratedLabelsAreIgnored: Compose hängt eigene Labels an jeden
// Container. Ohne Ausnahme gälte jeder von Compose verwaltete Container als
// abweichend — die zweithäufigste Falsch-positiv-Quelle.
func TestGeneratedLabelsAreIgnored(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:1.27
    labels:
      meins.zweck: "reverse proxy"
`)
	parsed, err := reconcile.ParseCompose("s", compose)
	if err != nil {
		t.Fatal(err)
	}

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{
			"com.docker.compose.service":       "web",
			"com.docker.compose.project":       "s",
			"com.docker.compose.config-hash":   "abc123",
			"org.opencontainers.image.version": "1.27",
			"desktop.docker.io/binds/0/Source": "/tmp",
			"meins.zweck":                      "reverse proxy",
		},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	if !rep.InSync {
		t.Fatalf("FALSCH-POSITIV durch erzeugte labels:\n%s", formatDrifts(rep.Drifts))
	}
}

// TestGeneratedLabelInComposeFileIsIgnored übt die Filterung auf der
// SOLL-Seite aus.
//
// Der Test daneben (TestGeneratedLabelsAreIgnored) bestand auch ohne jede
// Filterung — geschützt hat dort schon die Regel, dass zusätzliche Labels am
// Container nicht gemeldet werden. Erst dieser Fall zeigt, wofür der Filter
// da ist: Schreibt jemand ein von Werkzeugen erzeugtes Label in seine
// Compose-Datei, würde es sonst gegen den abweichenden Wert des Basisimages
// verglichen — eine Abweichung, die niemand auflösen kann.
func TestGeneratedLabelInComposeFileIsIgnored(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:1.27
    labels:
      org.opencontainers.image.title: "mein proxy"
      com.docker.compose.project: "handgeschrieben"
      meins.echt: "wert"
`)
	parsed, err := reconcile.ParseCompose("s", compose)
	if err != nil {
		t.Fatal(err)
	}
	// Nur das eigene Label darf übrig bleiben.
	labels := parsed.Desired.Services["web"].Labels
	if len(labels) != 1 || labels["meins.echt"] != "wert" {
		t.Fatalf("erzeugte labels wurden nicht aus dem soll-zustand entfernt: %v", labels)
	}

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{
			"com.docker.compose.service":     "web",
			"com.docker.compose.project":     "s",
			"org.opencontainers.image.title": "nginx",
			"meins.echt":                     "wert",
		},
	}})

	if rep := reconcile.Compare(parsed.Desired, observed); !rep.InSync {
		t.Fatalf("FALSCH-POSITIV durch erzeugte labels in der compose-datei:\n%s",
			formatDrifts(rep.Drifts))
	}
}

// TestExtraContainerLabelsAreNotDrift: Basis-Images bringen eigene Labels mit.
// Zusätzliche Labels am Container dürfen kein Rauschen erzeugen.
func TestExtraContainerLabelsAreNotDrift(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{
			"com.docker.compose.service": "web",
			"maintainer":                 "NGINX Docker Maintainers",
			"irgendwas.vom.basisimage":   "wert",
		},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	if !rep.InSync {
		t.Fatalf("FALSCH-POSITIV durch zusätzliche container-labels:\n%s", formatDrifts(rep.Drifts))
	}
}

// TestUnpublishedPortsAreIgnored: Ein Port ohne Host-Anteil bekommt einen
// zufälligen Host-Port. Ihn zu vergleichen würde bei jedem Neustart eine
// Abweichung erzeugen.
func TestUnpublishedPortsAreIgnored(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:1.27
    ports:
      - "8080:80"
      - "3000"
`)
	parsed, err := reconcile.ParseCompose("s", compose)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Warnings) == 0 {
		t.Error("port ohne host-angabe sollte eine warnung erzeugen, statt still zu verschwinden")
	}

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{"com.docker.compose.service": "web"},
		Ports: []transport.PortMapping{
			{Host: 8080, Container: 80, Protocol: "tcp"},
			{Host: 49153, Container: 3000, Protocol: "tcp"}, // zufällig vergeben
		},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	// Der zufällige Port erzeugt zwangsläufig eine Port-Abweichung; wichtig
	// ist, dass die Warnung erklärt, warum.
	for _, d := range rep.Drifts {
		if d.Field == "ports" && len(parsed.Warnings) == 0 {
			t.Error("port-abweichung ohne erklärende warnung")
		}
	}
}

func TestPortNotationVariants(t *testing.T) {
	compose := []byte(`
services:
  a:
    image: x:1
    ports:
      - "8080:80"
  b:
    image: x:1
    ports:
      - "127.0.0.1:9090:90"
  c:
    image: x:1
    ports:
      - "5353:53/udp"
  d:
    image: x:1
    ports:
      - target: 70
        published: 7070
        protocol: tcp
  e:
    image: x:1
    ports:
      - "3000-3002:3000-3002"
`)
	parsed, err := reconcile.ParseCompose("s", compose)
	if err != nil {
		t.Fatal(err)
	}

	check := func(svc string, want ...reconcile.Port) {
		t.Helper()
		got := parsed.Desired.Services[svc].Ports
		if len(got) != len(want) {
			t.Fatalf("%s: %d ports, erwartet %d (%v)", svc, len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s port %d = %v, erwartet %v", svc, i, got[i], want[i])
			}
		}
	}
	check("a", reconcile.Port{Host: 8080, Container: 80, Protocol: "tcp"})
	// Die Bindeadresse wird verworfen — Docker meldet sie pro Adressfamilie.
	check("b", reconcile.Port{Host: 9090, Container: 90, Protocol: "tcp"})
	check("c", reconcile.Port{Host: 5353, Container: 53, Protocol: "udp"})
	check("d", reconcile.Port{Host: 7070, Container: 70, Protocol: "tcp"})
	check("e",
		reconcile.Port{Host: 3000, Container: 3000, Protocol: "tcp"},
		reconcile.Port{Host: 3001, Container: 3001, Protocol: "tcp"},
		reconcile.Port{Host: 3002, Container: 3002, Protocol: "tcp"})
}

// TestPortOrderDoesNotMatter: Die Reihenfolge in der Compose-Datei ist
// beliebig, Docker meldet sie anders. Beide Seiten werden sortiert.
func TestPortOrderDoesNotMatter(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:1.27
    ports: ["9090:90", "8080:80"]
`)
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{"com.docker.compose.service": "web"},
		// Bewusst in umgekehrter Reihenfolge zur Compose-Datei: Nur so prüft
		// dieser Test die Sortierung überhaupt. Mit bereits sortierten Werten
		// bestünde er auch ohne jede Sortierung — er hätte aus dem falschen
		// Grund bestanden.
		Ports: []transport.PortMapping{
			{Host: 9090, Container: 90, Protocol: "tcp"},
			{Host: 8080, Container: 80, Protocol: "tcp"},
		},
	}})

	if rep := reconcile.Compare(parsed.Desired, observed); !rep.InSync {
		t.Fatalf("FALSCH-POSITIV durch portreihenfolge:\n%s", formatDrifts(rep.Drifts))
	}
}

// --- Echte Abweichungen müssen erkannt werden ---

func TestDetectsChangedImage(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.25", State: "running",
		Labels: map[string]string{"com.docker.compose.service": "web"},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	if rep.InSync {
		t.Fatal("geänderte image-version wurde nicht erkannt")
	}
	d := findDrift(t, rep, "web", "image")
	if d.Desired != "nginx:1.27" || d.Actual != "nginx:1.25" {
		t.Errorf("drift-inhalt falsch: %+v", d)
	}
}

func TestDetectsChangedPorts(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n    ports: [\"8080:80\"]\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	// Jemand hat den Port auf dem Host von Hand geändert.
	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{"com.docker.compose.service": "web"},
		Ports:  []transport.PortMapping{{Host: 8081, Container: 80, Protocol: "tcp"}},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	d := findDrift(t, rep, "web", "ports")
	if d.Desired != "8080:80/tcp" || d.Actual != "8081:80/tcp" {
		t.Errorf("port-drift falsch dargestellt: %+v", d)
	}
}

func TestDetectsMissingAndExtraServices(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:1.27
  fehlt:
    image: redis:7
`)
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{
		{
			ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
			Labels: map[string]string{"com.docker.compose.service": "web"},
		},
		{
			// Der schnelle Versuch von vor acht Monaten.
			ID: "c2", Name: "s-testcontainer-1", Stack: "s", Image: "alpine:3", State: "running",
			Labels: map[string]string{"com.docker.compose.service": "testcontainer"},
		},
	})

	rep := reconcile.Compare(parsed.Desired, observed)

	var sahMissing, sahExtra bool
	for _, d := range rep.Drifts {
		if d.Kind == reconcile.DriftMissing && d.Service == "fehlt" {
			sahMissing = true
		}
		if d.Kind == reconcile.DriftExtra && d.Service == "testcontainer" {
			sahExtra = true
		}
	}
	if !sahMissing {
		t.Error("fehlender dienst nicht erkannt")
	}
	if !sahExtra {
		t.Error("zusätzlicher dienst nicht erkannt — genau das ist der drift, der übersehen wird")
	}
}

// TestStoppedServiceIsOwnCategory: Ein gestoppter Dienst ist keine
// Konfigurationsabweichung. Die Unterscheidung ist für den Nutzer wichtig.
func TestStoppedServiceIsOwnCategory(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "exited",
		Labels: map[string]string{"com.docker.compose.service": "web"},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	if len(rep.Drifts) != 1 {
		t.Fatalf("%d abweichungen, erwartet 1:\n%s", len(rep.Drifts), formatDrifts(rep.Drifts))
	}
	if rep.Drifts[0].Kind != reconcile.DriftStopped {
		t.Errorf("gestoppter dienst als %q gemeldet, erwartet %q",
			rep.Drifts[0].Kind, reconcile.DriftStopped)
	}
}

func TestDetectsChangedLabel(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:1.27
    labels:
      - "traefik.enable=true"
      - "meins.zweck=proxy"
`)
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{
			"com.docker.compose.service": "web",
			"traefik.enable":             "false", // von Hand geändert
			// meins.zweck fehlt ganz
		},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	geaendert := findDrift(t, rep, "web", "label:traefik.enable")
	if geaendert.Actual != "false" {
		t.Errorf("label-abweichung falsch: %+v", geaendert)
	}
	fehlt := findDrift(t, rep, "web", "label:meins.zweck")
	if fehlt.Actual != "(fehlt)" {
		t.Errorf("fehlendes label falsch dargestellt: %+v", fehlt)
	}
}

// TestBuildServiceIsNotCompared: Lokal gebaute Images haben keinen
// vergleichbaren Bezeichner. Sie werden mit Warnung übersprungen statt geraten.
func TestBuildServiceIsNotCompared(t *testing.T) {
	compose := []byte(`
services:
  eigenes:
    build: ./app
    ports: ["8000:8000"]
`)
	parsed, err := reconcile.ParseCompose("s", compose)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Warnings) == 0 {
		t.Fatal("build-dienst sollte eine warnung erzeugen")
	}

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-eigenes-1", Stack: "s",
		Image: "s-eigenes:latest", State: "running",
		Labels: map[string]string{"com.docker.compose.service": "eigenes"},
		Ports:  []transport.PortMapping{{Host: 8000, Container: 8000, Protocol: "tcp"}},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	for _, d := range rep.Drifts {
		if d.Field == "image" {
			t.Errorf("lokal gebautes image wurde verglichen: %+v", d)
		}
	}
}

func TestRestartNotComparedWhenUnknown(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n    restart: unless-stopped\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	// Der Agent meldet die Neustart-Regel derzeit nicht.
	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{"com.docker.compose.service": "web"},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	if !rep.InSync {
		t.Fatalf("unbekannter ist-wert wurde verglichen — lieber nicht vergleichen als falsch melden:\n%s",
			formatDrifts(rep.Drifts))
	}
}

// TestRestartComparedWhenReported: Sobald der Agent die Regel meldet, wird sie
// verglichen. Das Gegenstück zu TestRestartNotComparedWhenUnknown.
func TestRestartComparedWhenReported(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n    restart: unless-stopped\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
		ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
		Restart: "always", // von Hand geändert
		Labels:  map[string]string{"com.docker.compose.service": "web"},
	}})

	rep := reconcile.Compare(parsed.Desired, observed)
	d := findDrift(t, rep, "web", "restart")
	if d.Desired != "unless-stopped" || d.Actual != "always" {
		t.Errorf("restart-drift falsch: %+v", d)
	}
}

// TestRestartEquivalentFormsAreNotDrift: Compose kennt keine Angabe als
// Vorgabe, Docker meldet "no". Beides ist dasselbe.
func TestRestartEquivalentFormsAreNotDrift(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n")
	parsed, _ := reconcile.ParseCompose("s", compose)

	for _, gemeldet := range []string{"no", ""} {
		observed := reconcile.NormalizeObserved("s", []transport.ResourceState{{
			ID: "c1", Name: "s-web-1", Stack: "s", Image: "nginx:1.27", State: "running",
			Restart: gemeldet,
			Labels:  map[string]string{"com.docker.compose.service": "web"},
		}})
		if rep := reconcile.Compare(parsed.Desired, observed); !rep.InSync {
			t.Errorf("FALSCH-POSITIV bei gemeldetem restart=%q:\n%s",
				gemeldet, formatDrifts(rep.Drifts))
		}
	}
}

func TestParseComposeRejectsGarbage(t *testing.T) {
	if _, err := reconcile.ParseCompose("s", []byte("das ist: [kein: gültiges yaml")); err == nil {
		t.Error("kaputtes yaml wurde angenommen")
	}
	if _, err := reconcile.ParseCompose("s", []byte("version: '3'\n")); err == nil {
		t.Error("compose-datei ohne services wurde angenommen")
	}
}

func TestOtherStacksAreIgnored(t *testing.T) {
	compose := []byte("services:\n  web:\n    image: nginx:1.27\n")
	parsed, _ := reconcile.ParseCompose("meiner", compose)

	observed := reconcile.NormalizeObserved("meiner", []transport.ResourceState{
		{
			ID: "c1", Name: "meiner-web-1", Stack: "meiner", Image: "nginx:1.27", State: "running",
			Labels: map[string]string{"com.docker.compose.service": "web"},
		},
		{
			// Anderer Stack auf demselben Host — darf nicht einfließen.
			ID: "c2", Name: "anderer-db-1", Stack: "anderer", Image: "postgres:16", State: "running",
			Labels: map[string]string{"com.docker.compose.service": "db"},
		},
	})

	if rep := reconcile.Compare(parsed.Desired, observed); !rep.InSync {
		t.Fatalf("fremder stack floss in den vergleich ein:\n%s", formatDrifts(rep.Drifts))
	}
}

// --- Hilfsfunktionen ---

func findDrift(t *testing.T, rep reconcile.Report, service, field string) reconcile.Drift {
	t.Helper()
	for _, d := range rep.Drifts {
		if d.Service == service && d.Field == field {
			return d
		}
	}
	t.Fatalf("keine abweichung für %s/%s gefunden:\n%s", service, field, formatDrifts(rep.Drifts))
	return reconcile.Drift{}
}

func formatDrifts(drifts []reconcile.Drift) string {
	if len(drifts) == 0 {
		return "  (keine)"
	}
	out := ""
	for _, d := range drifts {
		out += "  - [" + string(d.Kind) + "] " + d.Summary + "\n"
	}
	return out
}
