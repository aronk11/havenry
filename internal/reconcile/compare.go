package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aronk11/havenry/internal/provider/docker"
	"github.com/aronk11/havenry/internal/transport"
)

// Observed ist der normalisierte Ist-Zustand eines Stacks auf einem Host.
type Observed struct {
	Stack    string
	Services map[string]Service
	// States hält den Laufzeitzustand je Dienst (running, exited …).
	States map[string]string
}

// NormalizeObserved bringt die Agent-Meldung in dieselbe Form wie den
// Soll-Zustand.
//
// Beide Seiten laufen durch dieselben Normalisierungsfunktionen — das ist der
// Grund, warum der Vergleich überhaupt aussagekräftig sein kann. Zwei getrennte
// Normalisierungen würden zwangsläufig auseinanderlaufen.
func NormalizeObserved(stack string, resources []transport.ResourceState) Observed {
	obs := Observed{
		Stack:    stack,
		Services: map[string]Service{},
		States:   map[string]string{},
	}

	for _, r := range resources {
		if r.Stack != stack {
			continue
		}
		// Der Dienstname steht im Compose-Label. Fehlt es, wird der
		// Containername verwendet — dann handelt es sich um einen von Hand
		// gestarteten Container, der ohnehin als zusätzlich auffällt.
		name := r.Labels[docker.LabelService]
		if name == "" {
			name = r.Name
		}

		svc := Service{
			Name:  name,
			Image: NormalizeImage(r.Image),
			// Ein leerer Wert bedeutet "nicht gemeldet", nicht "keine Regel".
			// Diese Unterscheidung ist wesentlich: normalizeRestart("")
			// ergäbe "no" und würde bei jedem Stack mit restart-Angabe eine
			// Abweichung erzeugen (ADR-0026).
			Restart: normalizeObservedRestart(r.Restart),
			Labels:  filterLabels(r.Labels),
		}
		for _, p := range r.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			svc.Ports = append(svc.Ports, Port{
				Host: p.Host, Container: p.Container, Protocol: proto,
			})
		}
		sortPorts(svc.Ports)

		obs.Services[name] = svc
		obs.States[name] = r.State
	}
	return obs
}

// normalizeObservedRestart erhält die Unterscheidung zwischen "unbekannt"
// (leer) und einer tatsächlich gemeldeten Regel.
func normalizeObservedRestart(r string) string {
	if strings.TrimSpace(r) == "" {
		return ""
	}
	return normalizeRestart(r)
}

func filterLabels(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if isGeneratedLabel(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DriftKind benennt die Art einer Abweichung.
type DriftKind string

const (
	// DriftMissing: im Repo beschrieben, auf dem Host nicht vorhanden.
	DriftMissing DriftKind = "missing"
	// DriftExtra: läuft auf dem Host, steht nicht im Repo.
	DriftExtra DriftKind = "extra"
	// DriftChanged: vorhanden, aber ein Feld weicht ab.
	DriftChanged DriftKind = "changed"
	// DriftStopped: im Repo beschrieben und vorhanden, läuft aber nicht.
	DriftStopped DriftKind = "stopped"
)

// Drift ist eine einzelne erkannte Abweichung.
type Drift struct {
	Kind    DriftKind `json:"kind"`
	Service string    `json:"service"`
	// Field benennt das abweichende Feld bei DriftChanged.
	Field string `json:"field,omitempty"`
	// Desired und Actual sind für Menschen lesbare Darstellungen.
	Desired string `json:"desired,omitempty"`
	Actual  string `json:"actual,omitempty"`
	// Summary ist die Zeile, die in der Oberfläche steht.
	Summary string `json:"summary"`
}

// Report ist das Ergebnis eines Vergleichs für einen Stack auf einem Host.
type Report struct {
	Stack  string  `json:"stack"`
	HostID string  `json:"host_id"`
	Drifts []Drift `json:"drifts"`
	// Warnings aus dem Einlesen der Compose-Datei.
	Warnings []string `json:"warnings,omitempty"`
	// InSync ist wahr, wenn keine Abweichungen vorliegen.
	InSync bool `json:"in_sync"`
}

// Compare vergleicht Soll und Ist semantisch.
//
// Verglichen wird ausschließlich, was auf beiden Seiten zuverlässig bekannt
// ist. Alles andere wäre ein Falsch-positiv per Konstruktion — und ein
// Falsch-positiv kostet mehr Vertrauen, als eine erkannte Abweichung Nutzen
// bringt.
func Compare(desired Desired, observed Observed) Report {
	rep := Report{Stack: desired.Stack}

	names := make([]string, 0, len(desired.Services))
	for name := range desired.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		want := desired.Services[name]
		got, exists := observed.Services[name]

		if !exists {
			rep.Drifts = append(rep.Drifts, Drift{
				Kind: DriftMissing, Service: name,
				Desired: want.Image,
				Summary: fmt.Sprintf("%s ist im Repo beschrieben, läuft aber nicht auf diesem Host", name),
			})
			continue
		}

		// Ein vorhandener, aber gestoppter Dienst ist eine eigene Kategorie:
		// Die Konfiguration stimmt, nur läuft er nicht. Das als
		// Konfigurationsabweichung zu melden wäre irreführend.
		if state := observed.States[name]; state != "" && state != "running" {
			rep.Drifts = append(rep.Drifts, Drift{
				Kind: DriftStopped, Service: name,
				Actual:  state,
				Summary: fmt.Sprintf("%s ist vorhanden, läuft aber nicht (%s)", name, state),
			})
		}

		rep.Drifts = append(rep.Drifts, compareService(name, want, got)...)
	}

	// Zusätzliche Dienste: laufen, stehen aber nicht im Repo. Das ist die
	// Abweichung, die am häufigsten übersehen wird — der schnelle Versuch von
	// vor acht Monaten.
	extra := make([]string, 0)
	for name := range observed.Services {
		if _, ok := desired.Services[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		rep.Drifts = append(rep.Drifts, Drift{
			Kind: DriftExtra, Service: name,
			Actual:  observed.Services[name].Image,
			Summary: fmt.Sprintf("%s läuft auf dem Host, steht aber nicht im Repo", name),
		})
	}

	rep.InSync = len(rep.Drifts) == 0
	return rep
}

func compareService(name string, want, got Service) []Drift {
	var out []Drift

	// Ein leerer Soll-Image-Wert bedeutet: lokal gebaut, nicht vergleichbar
	// (siehe ParseCompose). Dann wird das Feld übersprungen statt geraten.
	if want.Image != "" && want.Image != got.Image {
		out = append(out, Drift{
			Kind: DriftChanged, Service: name, Field: "image",
			Desired: want.Image, Actual: got.Image,
			Summary: fmt.Sprintf("%s: Image weicht ab — Repo %s, Host %s",
				name, want.Image, got.Image),
		})
	}

	if d := comparePorts(name, want.Ports, got.Ports); d != nil {
		out = append(out, *d)
	}

	// Restart wird nur verglichen, wenn der Ist-Wert bekannt ist. Der Agent
	// meldet ihn derzeit nicht — lieber nicht vergleichen als falsch melden.
	if got.Restart != "" && want.Restart != got.Restart {
		out = append(out, Drift{
			Kind: DriftChanged, Service: name, Field: "restart",
			Desired: want.Restart, Actual: got.Restart,
			Summary: fmt.Sprintf("%s: Neustart-Regel weicht ab — Repo %s, Host %s",
				name, want.Restart, got.Restart),
		})
	}

	out = append(out, compareLabels(name, want.Labels, got.Labels)...)
	return out
}

func comparePorts(name string, want, got []Port) *Drift {
	if portsEqual(want, got) {
		return nil
	}
	return &Drift{
		Kind: DriftChanged, Service: name, Field: "ports",
		Desired: formatPorts(want), Actual: formatPorts(got),
		Summary: fmt.Sprintf("%s: Ports weichen ab — Repo [%s], Host [%s]",
			name, formatPorts(want), formatPorts(got)),
	}
}

func portsEqual(a, b []Port) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func formatPorts(p []Port) string {
	if len(p) == 0 {
		return "keine"
	}
	parts := make([]string, len(p))
	for i, port := range p {
		parts[i] = port.String()
	}
	return strings.Join(parts, ", ")
}

// compareLabels meldet nur Labels, die im Repo stehen und am Container fehlen
// oder abweichen.
//
// Zusätzliche Labels am Container werden NICHT gemeldet: Basis-Images bringen
// eigene mit, und die Liste der von Werkzeugen gesetzten Labels ist nicht
// abschließend bekannt. Sie zu melden hieße, dauerhaft Rauschen zu erzeugen.
func compareLabels(name string, want, got map[string]string) []Drift {
	if len(want) == 0 {
		return nil
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []Drift
	for _, k := range keys {
		gotVal, ok := got[k]
		if !ok {
			out = append(out, Drift{
				Kind: DriftChanged, Service: name, Field: "label:" + k,
				Desired: want[k], Actual: "(fehlt)",
				Summary: fmt.Sprintf("%s: Label %q fehlt am Container", name, k),
			})
			continue
		}
		if gotVal != want[k] {
			out = append(out, Drift{
				Kind: DriftChanged, Service: name, Field: "label:" + k,
				Desired: want[k], Actual: gotVal,
				Summary: fmt.Sprintf("%s: Label %q weicht ab — Repo %q, Host %q",
					name, k, want[k], gotVal),
			})
		}
	}
	return out
}
