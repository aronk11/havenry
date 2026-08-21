// Package provider definiert die Schnittstelle, über die die Plattform mit einer
// Laufzeitumgebung spricht. Siehe ADR-0008 (Provider-Interface statt Plugin-System)
// und ADR-0011 (lesende Provider wie Proxmox).
package provider

import "context"

// Capability beschreibt, was ein Provider kann. Lesende Provider (Proxmox, ADR-0011)
// melden nur CapRead — der Aufrufer prüft die Capability, statt eine Methode
// aufzurufen, die dann "nicht unterstützt" zurückgibt.
type Capability uint8

const (
	CapRead      Capability = 1 << iota
	CapLifecycle            // starten, stoppen, neu starten
	CapApply                // Soll-Zustand herstellen
	CapLogs
)

// Resource ist die kleinste beobachtbare Einheit (ein Container, eine VM, ein LXC).
type Resource struct {
	ID       string
	Name     string
	Kind     string // "container" | "vm" | "lxc"
	Stack    string
	Image    string
	Digest   string
	State    string
	Health   string
	Restart  string
	Ports    []Port
	Labels   map[string]string
	Restarts int
}

type Port struct {
	Host      int
	Container int
	Protocol  string
}

// Provider liest den Ist-Zustand einer Laufzeitumgebung und führt — sofern die
// Capabilities es erlauben — Aktionen darauf aus.
type Provider interface {
	Name() string
	Capabilities() Capability

	// Observe liefert den vollständigen Ist-Zustand. Muss auch bei teilweise
	// nicht erreichbaren Ressourcen ein brauchbares Ergebnis liefern.
	Observe(ctx context.Context) ([]Resource, error)
}

// LifecycleProvider wird implementiert, wenn CapLifecycle gesetzt ist.
// Alle Methoden müssen idempotent sein — Kommandos können nach einem
// Verbindungsabbruch doppelt ankommen (ADR-0013).
type LifecycleProvider interface {
	Provider
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
}
