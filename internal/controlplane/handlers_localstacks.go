package controlplane

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aronk11/havenry/internal/reconcile"
	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/transport"
)

// Lokale Stacks (ADR-0034): Compose-Definitionen, die Havenry selbst in der
// Datenbank hält statt aus einem Git-Repo zu lesen. Laufen ab dem Punkt, an
// dem der Compose-Inhalt feststeht, durch denselben Weg wie Git-Stacks
// (applyStack, compareCompose) — der Agent kennt den Unterschied nicht.

// localStackView ist die API-Form eines store.LocalStack.
type localStackView struct {
	Name        string    `json:"name"`
	HostID      string    `json:"host_id"`
	ComposeYAML string    `json:"compose_yaml"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}

func localStackToView(st store.LocalStack) localStackView {
	return localStackView{
		Name: st.Name, HostID: st.HostID, ComposeYAML: st.ComposeYAML,
		CreatedAt: st.CreatedAt, UpdatedAt: st.UpdatedAt, UpdatedBy: st.UpdatedBy,
	}
}

func (s *Server) listLocalStacks(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("hostID")
	if !s.requireHostAccess(w, r, hostID) {
		return
	}
	stacks, err := s.store.LocalStacksForHost(r.Context(), hostID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]localStackView, 0, len(stacks))
	for _, st := range stacks {
		out = append(out, localStackToView(st))
	}
	writeJSON(w, http.StatusOK, map[string]any{"local_stacks": out})
}

func (s *Server) getLocalStack(w http.ResponseWriter, r *http.Request) {
	hostID, name := r.PathValue("hostID"), r.PathValue("name")
	if !s.requireHostAccess(w, r, hostID) {
		return
	}
	st, err := s.store.LocalStackByName(r.Context(), hostID, name)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, localStackToView(st))
}

type localStackBody struct {
	Name        string `json:"name"`
	ComposeYAML string `json:"compose_yaml"`
}

func (s *Server) createLocalStack(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("hostID")
	if !s.requireHostAccess(w, r, hostID) {
		return
	}
	if _, err := s.store.HostByID(r.Context(), hostID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	var body localStackBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name fehlt"))
		return
	}
	if _, err := reconcile.ParseCompose(body.Name, []byte(body.ComposeYAML)); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("compose-inhalt ungültig: %w", err))
		return
	}

	now := time.Now().UTC()
	id := identityFrom(r.Context())
	st := store.LocalStack{
		ID: hostID + ":" + body.Name, HostID: hostID, Name: body.Name,
		ComposeYAML: body.ComposeYAML, CreatedAt: now, UpdatedAt: now, UpdatedBy: id.Username,
	}
	if err := s.store.CreateLocalStack(r.Context(), st); err != nil {
		if errors.Is(err, store.ErrLocalStackExists) {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: now, HostID: hostID, Kind: "localstack.created", Actor: id.Actor(),
		Summary: fmt.Sprintf("Lokaler Stack %q angelegt", body.Name),
	})
	writeJSON(w, http.StatusCreated, localStackToView(st))
}

func (s *Server) updateLocalStack(w http.ResponseWriter, r *http.Request) {
	hostID, name := r.PathValue("hostID"), r.PathValue("name")
	if !s.requireHostAccess(w, r, hostID) {
		return
	}

	existing, err := s.store.LocalStackByName(r.Context(), hostID, name)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	var body localStackBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := reconcile.ParseCompose(name, []byte(body.ComposeYAML)); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("compose-inhalt ungültig: %w", err))
		return
	}

	id := identityFrom(r.Context())
	existing.ComposeYAML = body.ComposeYAML
	existing.UpdatedAt = time.Now().UTC()
	existing.UpdatedBy = id.Username
	if err := s.store.UpdateLocalStack(r.Context(), existing); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: existing.UpdatedAt, HostID: hostID, Kind: "localstack.updated", Actor: id.Actor(),
		Summary: fmt.Sprintf("Lokaler Stack %q bearbeitet", name),
	})
	writeJSON(w, http.StatusOK, localStackToView(existing))
}

func (s *Server) deleteLocalStack(w http.ResponseWriter, r *http.Request) {
	hostID, name := r.PathValue("hostID"), r.PathValue("name")
	if !s.requireHostAccess(w, r, hostID) {
		return
	}
	if err := s.store.DeleteLocalStack(r.Context(), hostID, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	id := identityFrom(r.Context())
	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), HostID: hostID, Kind: "localstack.deleted", Actor: id.Actor(),
		Summary: fmt.Sprintf("Lokaler Stack %q gelöscht — Container auf dem Host laufen weiter, "+
			"bis sie einzeln gestoppt werden", name),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht", "name": name})
}

// applyLocalStack sendet den gespeicherten Compose-Inhalt an den Agenten.
//
// Derselbe Weg wie revertStack für Git-Stacks (handlers_resolve.go) — nur
// dass der Inhalt aus der Datenbank statt aus dem Git-Checkout kommt.
func (s *Server) applyLocalStack(w http.ResponseWriter, r *http.Request) {
	hostID, name := r.PathValue("hostID"), r.PathValue("name")
	if !s.requireHostAccess(w, r, hostID) {
		return
	}

	st, err := s.store.LocalStackByName(r.Context(), hostID, name)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	id := identityFrom(r.Context())
	res, err := s.applyStack(r.Context(), hostID, name, []byte(st.ComposeYAML))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), HostID: hostID, Kind: "localstack.applied", Actor: id.Actor(),
		Summary: fmt.Sprintf("Lokaler Stack %q angewendet (%s)", name, res.Status),
	})

	code := http.StatusOK
	if res.Status == transport.StatusFailed {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, res)
}
