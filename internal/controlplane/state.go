package controlplane

import (
	"sort"
	"sync"
	"time"

	"github.com/aronk11/havenry/internal/transport"
)

// stateCache hält den zuletzt gemeldeten Ist-Zustand je Host.
//
// Bewusst nur im Arbeitsspeicher, nicht in der Datenbank: Der Ist-Zustand ist
// flüchtig und wird nach jedem Neustart binnen Sekunden neu gemeldet. Ihn zu
// persistieren würde nur Schreiblast erzeugen und beim Start veraltete Daten
// anzeigen — schlimmer als gar keine (ADR-0018: lieber Unwissen eingestehen
// als falschen Zustand behaupten).
type stateCache struct {
	mu sync.RWMutex
	// byHost bildet HostID auf den letzten Report ab.
	byHost map[string]hostState
}

type hostState struct {
	ObservedAt time.Time
	Resources  []transport.ResourceState
	Metrics    *transport.MetricsReport
}

func newStateCache() *stateCache {
	return &stateCache{byHost: make(map[string]hostState)}
}

func (c *stateCache) putState(hostID string, r transport.StateReport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.byHost[hostID]
	st.ObservedAt = r.ObservedAt
	st.Resources = r.Resources
	c.byHost[hostID] = st
}

func (c *stateCache) putMetrics(hostID string, m transport.MetricsReport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.byHost[hostID]
	st.Metrics = &m
	c.byHost[hostID] = st
}

func (c *stateCache) get(hostID string) (hostState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st, ok := c.byHost[hostID]
	return st, ok
}

// forget entfernt den Zustand eines Hosts. Wird beim Trennen aufgerufen —
// ein Container-Zustand von einem Host, der seit einer Stunde weg ist,
// ist Fehlinformation, keine Information.
func (c *stateCache) forget(hostID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byHost, hostID)
}

// containerView ist die API-Darstellung eines Containers.
type containerView struct {
	ID       string                  `json:"id"`
	HostID   string                  `json:"host_id"`
	HostName string                  `json:"host_name"`
	Name     string                  `json:"name"`
	Stack    string                  `json:"stack,omitempty"`
	Service  string                  `json:"service,omitempty"`
	Image    string                  `json:"image,omitempty"`
	State    string                  `json:"state"`
	Health   string                  `json:"health,omitempty"`
	Restarts int                     `json:"restarts,omitempty"`
	Ports    []transport.PortMapping `json:"ports,omitempty"`
}

// stackView bündelt Container eines Compose-Projekts.
type stackView struct {
	Name       string          `json:"name"`
	HostID     string          `json:"host_id"`
	HostName   string          `json:"host_name"`
	Containers []containerView `json:"containers"`
	// Summary fasst zusammen, was auf einen Blick zählt.
	Running int `json:"running"`
	Total   int `json:"total"`
}

// sortStacks sorgt für eine stabile Reihenfolge in der Oberfläche.
// Ohne feste Sortierung springen Einträge bei jedem Abruf — das wirkt wie ein
// Fehler, auch wenn nichts passiert ist.
func sortStacks(stacks []stackView) {
	sort.Slice(stacks, func(i, j int) bool {
		if stacks[i].HostName != stacks[j].HostName {
			return stacks[i].HostName < stacks[j].HostName
		}
		return stacks[i].Name < stacks[j].Name
	})
	for i := range stacks {
		cs := stacks[i].Containers
		sort.Slice(cs, func(a, b int) bool { return cs[a].Name < cs[b].Name })
	}
}
