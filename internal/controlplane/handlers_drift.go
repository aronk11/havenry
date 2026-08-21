package controlplane

import (
	"net/http"
	"sort"
	"time"

	"github.com/aronk11/havenry/internal/gitsync"
	"github.com/aronk11/havenry/internal/reconcile"
	"github.com/aronk11/havenry/internal/store"
)

// driftView ist ein Vergleichsergebnis für einen Stack auf einem Host.
type driftView struct {
	Stack    string            `json:"stack"`
	HostID   string            `json:"host_id"`
	HostName string            `json:"host_name"`
	Mode     string            `json:"mode"`
	InSync   bool              `json:"in_sync"`
	Drifts   []reconcile.Drift `json:"drifts"`
	Warnings []string          `json:"warnings,omitempty"`
	// Error steht, wenn der Vergleich nicht möglich war (etwa eine
	// unlesbare Compose-Datei). Bewusst getrennt von "keine Abweichung" —
	// "konnte nicht prüfen" ist nicht dasselbe wie "alles in Ordnung"
	// (ADR-0018).
	Error string `json:"error,omitempty"`
	// HostConnected: Ohne verbundenen Host gibt es keinen Ist-Zustand, und
	// ein Vergleich gegen nichts würde jeden Dienst als fehlend melden.
	HostConnected bool `json:"host_connected"`
	// CanRevert meldet, ob der Host Stacks anwenden kann (docker compose
	// vorhanden, ADR-0027). Ohne das bleibt der Knopf aus, statt beim Klick
	// zu scheitern.
	CanRevert bool `json:"can_revert"`
	// CanAdopt meldet Schreibzugriff aufs Repo (ADR-0028).
	CanAdopt bool `json:"can_adopt"`
	// AdoptCheckedAt sagt, wann der Schreibzugriff zuletzt geprüft wurde.
	//
	// Sichtbar, weil "adopt ist ausgegraut" sonst nicht nachvollziehbar wäre:
	// Der Nutzer soll erkennen können, ob die Angabe frisch ist oder aus dem
	// Zwischenspeicher stammt (siehe repoManager.CanPush).
	AdoptCheckedAt time.Time `json:"adopt_checked_at,omitempty"`
}

// listDrift vergleicht den Soll-Zustand aus Git mit dem gemeldeten Ist-Zustand.
func (s *Server) listDrift(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.store.Hosts(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	hosts = visibleHosts(identityFrom(r.Context()), hosts)

	disc, _, _, _, _ := s.repo.Snapshot()
	if len(disc.Stacks) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"drift":   []driftView{},
			"hinweis": "kein Repository verbunden oder keine Stacks erkannt",
		})
		return
	}

	// Einmal je Abruf prüfen, nicht je Stack: Der Push-Test kostet einen
	// Netzwerkaufruf.
	canAdopt, adoptCheckedAt := s.repo.CanPush(r.Context())

	out := make([]driftView, 0)
	for _, h := range hosts {
		for _, st := range s.repo.StacksForHost(h.Hostname) {
			v := s.compareStack(h, st)
			v.CanAdopt = canAdopt
			v.AdoptCheckedAt = adoptCheckedAt
			out = append(out, v)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		// Abweichungen zuerst — das ist, wonach jemand sucht.
		if out[i].InSync != out[j].InSync {
			return !out[i].InSync
		}
		if out[i].HostName != out[j].HostName {
			return out[i].HostName < out[j].HostName
		}
		return out[i].Stack < out[j].Stack
	})
	writeJSON(w, http.StatusOK, map[string]any{"drift": out})
}

// compareStack führt den Vergleich für einen Stack auf einem Host durch.
func (s *Server) compareStack(h store.Host, st gitsync.Stack) driftView {
	v := driftView{
		Stack: st.Name, HostID: h.ID, HostName: h.Hostname, Mode: string(st.Mode),
	}
	sess, connected := s.hub.Session(h.ID)
	v.HostConnected = connected
	if connected {
		v.CanRevert = sess.HasCapability("apply")
	}

	data, err := gitsync.ReadCompose(s.repo.workDir, st.ComposePath)
	if err != nil {
		v.Error = "compose-datei nicht lesbar: " + err.Error()
		return v
	}
	parsed, err := reconcile.ParseCompose(st.Name, data)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	for _, warn := range parsed.Warnings {
		v.Warnings = append(v.Warnings, warn.Error())
	}

	if !v.HostConnected {
		// Ohne Ist-Zustand wird nicht verglichen. Alles als fehlend zu melden
		// wäre die schlimmste Sorte Falsch-positiv: eine Seite voller
		// Abweichungen, sobald ein Host kurz offline ist.
		v.Error = "host nicht verbunden — kein Ist-Zustand verfügbar"
		return v
	}

	hostState, ok := s.state.get(h.ID)
	if !ok {
		v.Error = "noch keine Zustandsmeldung von diesem Host"
		return v
	}

	observed := reconcile.NormalizeObserved(st.Name, hostState.Resources)
	rep := reconcile.Compare(parsed.Desired, observed)
	v.Drifts = rep.Drifts
	v.InSync = rep.InSync
	if v.Drifts == nil {
		v.Drifts = []reconcile.Drift{}
	}
	return v
}
