package controlplane

import (
	"net/http"
	"strconv"
)

// Hosts, Enrollment-Token und Ereignisprotokoll.
//
// Die Sichtbarkeit wird hier gefiltert, nicht erst in der Oberfläche: Ein
// beschränkter Nutzer soll fremde Hosts gar nicht erst in der Antwort sehen
// (ADR-0022).

func (s *Server) listHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.store.Hosts(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Ein beschränkter Nutzer sieht fremde Hosts gar nicht erst (ADR-0022).
	hosts = visibleHosts(identityFrom(r.Context()), hosts)

	out := make([]hostView, 0, len(hosts))
	for _, h := range hosts {
		_, connected := s.hub.Session(h.ID)
		v := hostView{
			ID: h.ID, Hostname: h.Hostname, Approved: h.Approved, Connected: connected,
			OS: h.OS, Arch: h.Arch, AgentVersion: h.AgentVersion,
			EnrolledAt: h.EnrolledAt, LastSeen: h.LastSeen,
		}
		if st, ok := s.state.get(h.ID); ok {
			v.Containers = len(st.Resources)
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": out})
}

func (s *Server) approveHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.enroll.Approve(r.Context(), id, identityFrom(r.Context()).Actor()); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	// Die offene Sitzung erfährt die Bestätigung sofort — sonst müsste der
	// Nutzer den Agenten neu starten, was niemand erwartet.
	s.hub.SetApproved(id, true)
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "host_id": id})
}

func (s *Server) issueToken(w http.ResponseWriter, r *http.Request) {
	tok, expires, err := s.enroll.IssueToken(r.Context(), identityFrom(r.Context()).Actor())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      tok,
		"expires_at": expires,
		"hinweis":    "Token wird nur einmal angezeigt und ist 15 Minuten gültig.",
	})
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	events, err := s.store.Events(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}
