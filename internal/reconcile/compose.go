// Package reconcile normalisiert Soll- und Ist-Zustand und vergleicht sie
// semantisch.
//
// Das ist das Herzstück des Produkts und sein größtes Qualitätsrisiko: Ein
// textueller Vergleich erzeugt Falsch-positive (`nginx` gegen `nginx:latest`,
// unterschiedliche Portnotationen, implizite Compose-Vorgaben, generierte
// Netzwerk- und Volume-Namen). Falsch-positive Abweichungen zerstören das
// Vertrauen der Nutzer schneller als jeder Absturz — siehe CONTRIBUTING.md.
package reconcile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Desired ist der normalisierte Soll-Zustand eines Stacks.
type Desired struct {
	Stack    string
	Services map[string]Service
}

// Service ist ein normalisierter Dienst — die Form, in der verglichen wird.
//
// Bewusst nur die Felder, die sich am laufenden Container zuverlässig ablesen
// lassen. Ein Feld, das wir aus der Compose-Datei kennen, aber am Ist-Zustand
// nicht sehen können, würde bei jedem Vergleich als Abweichung erscheinen —
// das wäre ein Falsch-positiv per Konstruktion.
type Service struct {
	Name string
	// Image ist der normalisierte Image-Bezeichner (siehe NormalizeImage).
	Image string
	// Ports sind nur die veröffentlichten Ports, sortiert.
	Ports []Port
	// Restart ist die normalisierte Neustart-Regel.
	Restart string
	// Labels enthält nur vom Nutzer gesetzte Labels; von Compose erzeugte
	// werden entfernt (siehe isGeneratedLabel).
	Labels map[string]string
}

// Port ist eine veröffentlichte Portzuordnung.
type Port struct {
	Host      int
	Container int
	Protocol  string
}

func (p Port) String() string {
	return fmt.Sprintf("%d:%d/%s", p.Host, p.Container, p.Protocol)
}

// composeFile bildet die Teile der Compose-Datei ab, die verglichen werden.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image   string `yaml:"image"`
	Ports   []any  `yaml:"ports"`
	Restart string `yaml:"restart"`
	Labels  any    `yaml:"labels"`
	// Build wird erkannt, aber nicht verglichen — siehe ParseCompose.
	Build any `yaml:"build"`
}

// ParseError beschreibt ein Problem in einer Compose-Datei.
type ParseError struct {
	Service string
	Message string
}

func (e ParseError) Error() string {
	if e.Service == "" {
		return e.Message
	}
	return e.Service + ": " + e.Message
}

// ParseResult ist das Ergebnis des Einlesens.
type ParseResult struct {
	Desired Desired
	// Warnings nennt Dienste, die nicht vollständig verglichen werden können.
	// Sie werden angezeigt, statt stillschweigend als abweichungsfrei zu gelten —
	// eine stille Lücke wäre schlimmer als eine sichtbare.
	Warnings []ParseError
}

// ParseCompose liest eine Compose-Datei und normalisiert sie.
func ParseCompose(stack string, data []byte) (ParseResult, error) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return ParseResult{}, fmt.Errorf("compose-datei unlesbar: %w", err)
	}
	if len(cf.Services) == 0 {
		return ParseResult{}, fmt.Errorf("compose-datei enthält keine services")
	}

	res := ParseResult{Desired: Desired{Stack: stack, Services: map[string]Service{}}}

	for name, cs := range cf.Services {
		svc := Service{Name: name, Restart: normalizeRestart(cs.Restart)}

		switch {
		case cs.Image != "":
			svc.Image = NormalizeImage(cs.Image)
		case cs.Build != nil:
			// Selbst gebaute Images tragen keinen vergleichbaren Bezeichner.
			// Der Dienst wird aufgenommen, das Image aber nicht verglichen.
			res.Warnings = append(res.Warnings, ParseError{
				Service: name,
				Message: "wird lokal gebaut (build:) — das Image wird nicht verglichen",
			})
		default:
			res.Warnings = append(res.Warnings, ParseError{
				Service: name,
				Message: "weder image noch build angegeben",
			})
		}

		ports, warn := parsePorts(cs.Ports)
		if warn != "" {
			res.Warnings = append(res.Warnings, ParseError{Service: name, Message: warn})
		}
		svc.Ports = ports
		svc.Labels = parseLabels(cs.Labels)

		res.Desired.Services[name] = svc
	}
	return res, nil
}

// NormalizeImage bringt einen Image-Bezeichner auf eine vergleichbare Form.
//
// Das ist die häufigste Quelle für Falsch-positive: Docker meldet
// `nginx:latest`, in der Compose-Datei steht `nginx`. Beides bezeichnet
// dasselbe. Ebenso ergänzt Docker die Registry (`docker.io/library/`), die
// der Nutzer nie schreibt.
func NormalizeImage(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}

	// Digest-Angabe (image@sha256:…) bleibt unverändert — sie ist eindeutig.
	if i := strings.Index(image, "@"); i > 0 {
		return image
	}

	name, tag := image, ""
	// Der Doppelpunkt einer Portangabe in der Registry (registry:5000/foo)
	// darf nicht als Tag-Trenner gelesen werden: nur nach dem letzten
	// Schrägstrich suchen.
	lastSlash := strings.LastIndex(image, "/")
	if i := strings.LastIndex(image, ":"); i > lastSlash {
		name, tag = image[:i], image[i+1:]
	}
	if tag == "" {
		tag = "latest"
	}

	// Registry-Vorsilben entfernen, die Docker ergänzt, der Nutzer aber nicht
	// schreibt.
	name = strings.TrimPrefix(name, "docker.io/")
	name = strings.TrimPrefix(name, "index.docker.io/")
	// Offizielle Images liegen unter library/, was niemand tippt.
	if strings.HasPrefix(name, "library/") && strings.Count(name, "/") == 1 {
		name = strings.TrimPrefix(name, "library/")
	}
	return name + ":" + tag
}

// normalizeRestart vereinheitlicht die Neustart-Regel.
//
// Compose kennt "no" als Vorgabe, Docker meldet einen leeren Wert oder "no".
// Ohne Normalisierung erschiene das als Abweichung.
func normalizeRestart(r string) string {
	r = strings.TrimSpace(strings.ToLower(r))
	switch r {
	case "", "no", "none":
		return "no"
	case "always", "unless-stopped", "on-failure":
		return r
	default:
		// on-failure:5 und ähnliche Formen auf den Grundwert bringen.
		if strings.HasPrefix(r, "on-failure") {
			return "on-failure"
		}
		return r
	}
}

// parsePorts liest die verschiedenen Portschreibweisen von Compose.
//
// Erlaubt sind unter anderem: 8080, "8080:80", "127.0.0.1:8080:80",
// "8080:80/udp", "3000-3005:3000-3005" und die ausführliche Abbildungsform.
// Nur veröffentlichte Ports werden verglichen — ein Port ohne Host-Anteil ist
// containerintern und am Ist-Zustand nicht als Veröffentlichung sichtbar.
func parsePorts(raw []any) ([]Port, string) {
	var out []Port
	var warn string

	for _, entry := range raw {
		switch v := entry.(type) {
		case int:
			// Kurzform "3000" bedeutet: Container-Port, zufälliger Host-Port.
			// Der Host-Port ist damit nicht vorhersagbar und nicht vergleichbar.
			warn = "port ohne host-angabe wird nicht verglichen (zufälliger host-port)"
			_ = v
		case uint64:
			warn = "port ohne host-angabe wird nicht verglichen (zufälliger host-port)"
		case string:
			p, ok, w := parsePortString(v)
			if w != "" {
				warn = w
			}
			if ok {
				out = append(out, p...)
			}
		case map[string]any:
			p, ok := parsePortMapping(v)
			if ok {
				out = append(out, p)
			}
		}
	}

	sortPorts(out)
	return out, warn
}

func parsePortString(s string) ([]Port, bool, string) {
	proto := "tcp"
	if i := strings.Index(s, "/"); i >= 0 {
		proto = strings.ToLower(s[i+1:])
		s = s[:i]
	}

	parts := strings.Split(s, ":")
	var hostPart, containerPart string
	switch len(parts) {
	case 1:
		// Nur Container-Port: Host-Port wird zufällig vergeben.
		return nil, false, "port ohne host-angabe wird nicht verglichen (zufälliger host-port)"
	case 2:
		hostPart, containerPart = parts[0], parts[1]
	case 3:
		// Form "127.0.0.1:8080:80" — die Bindeadresse wird nicht verglichen,
		// weil Docker sie im Ist-Zustand pro Adressfamilie mehrfach meldet.
		hostPart, containerPart = parts[1], parts[2]
	default:
		return nil, false, "portangabe " + s + " nicht verstanden"
	}

	// Bereiche wie 3000-3005:3000-3005 auflösen.
	if strings.Contains(hostPart, "-") || strings.Contains(containerPart, "-") {
		hs, he, ok1 := parseRange(hostPart)
		cs, ce, ok2 := parseRange(containerPart)
		if !ok1 || !ok2 || (he-hs) != (ce-cs) {
			return nil, false, "portbereich " + s + " nicht verstanden"
		}
		var out []Port
		for i := 0; i <= he-hs; i++ {
			out = append(out, Port{Host: hs + i, Container: cs + i, Protocol: proto})
		}
		return out, true, ""
	}

	h, err1 := strconv.Atoi(strings.TrimSpace(hostPart))
	c, err2 := strconv.Atoi(strings.TrimSpace(containerPart))
	if err1 != nil || err2 != nil {
		return nil, false, "portangabe " + s + " nicht verstanden"
	}
	return []Port{{Host: h, Container: c, Protocol: proto}}, true, ""
}

func parseRange(s string) (int, int, bool) {
	if i := strings.Index(s, "-"); i > 0 {
		a, err1 := strconv.Atoi(s[:i])
		b, err2 := strconv.Atoi(s[i+1:])
		if err1 != nil || err2 != nil || b < a {
			return 0, 0, false
		}
		return a, b, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, false
	}
	return n, n, true
}

// parsePortMapping liest die ausführliche Form:
//
//	ports:
//	  - target: 80
//	    published: 8080
//	    protocol: tcp
func parsePortMapping(m map[string]any) (Port, bool) {
	target, ok1 := toInt(m["target"])
	published, ok2 := toInt(m["published"])
	if !ok1 || !ok2 {
		return Port{}, false
	}
	proto := "tcp"
	if p, ok := m["protocol"].(string); ok && p != "" {
		proto = strings.ToLower(p)
	}
	return Port{Host: published, Container: target, Protocol: proto}, true
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true //nolint:gosec // G115: nur Portnummern und Zähler aus Compose, weit unter 2^31
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

// parseLabels liest Labels in beiden Schreibweisen (Liste und Abbildung).
func parseLabels(raw any) map[string]string {
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]any:
		for k, val := range v {
			if isGeneratedLabel(k) {
				continue
			}
			out[k] = fmt.Sprint(val)
		}
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			k, val := s, ""
			if i := strings.Index(s, "="); i > 0 {
				k, val = s[:i], s[i+1:]
			}
			if isGeneratedLabel(k) {
				continue
			}
			out[k] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isGeneratedLabel erkennt Labels, die Compose selbst setzt.
//
// Sie stehen am laufenden Container, aber nicht in der Compose-Datei — ohne
// diese Ausnahme würde jeder von Compose verwaltete Container als abweichend
// gelten. Das ist die zweithäufigste Falsch-positiv-Quelle nach den Images.
func isGeneratedLabel(k string) bool {
	return strings.HasPrefix(k, "com.docker.compose.") ||
		strings.HasPrefix(k, "org.opencontainers.image.") ||
		strings.HasPrefix(k, "desktop.docker.io/")
}

func sortPorts(p []Port) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].Host != p[j].Host {
			return p[i].Host < p[j].Host
		}
		if p[i].Container != p[j].Container {
			return p[i].Container < p[j].Container
		}
		return p[i].Protocol < p[j].Protocol
	})
}
