package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aronk11/havenry/internal/gitsync"
	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/transport"
)

// resolveDrift setzt ADR-0004 um: Der Nutzer entscheidet je Abweichung, in
// welche Richtung sie aufgelöst wird. Automatisch passiert nichts.
func (s *Server) resolveDrift(w http.ResponseWriter, r *http.Request) {
	hostID, stack, action := r.PathValue("hostID"), r.PathValue("stack"), r.PathValue("action")

	if !s.requireHostAccess(w, r, hostID) {
		return
	}

	var body struct {
		// Service und Field grenzen bei adopt die Änderung ein.
		Service string `json:"service"`
		Field   string `json:"field"`
	}
	if r.ContentLength > 0 {
		if err := decodeBody(r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}

	switch action {
	case "revert":
		s.revertStack(w, r, hostID, stack)
	case "adopt":
		s.adoptChange(w, r, hostID, stack, body.Service, body.Field)
	default:
		writeErr(w, http.StatusBadRequest,
			fmt.Errorf("aktion %q unbekannt (erlaubt: revert, adopt)", action))
	}
}

// findStack sucht den Soll-Stack für einen Host.
func (s *Server) findStack(ctx context.Context, hostID, stackName string) (store.Host, gitsync.Stack, error) {
	host, err := s.store.HostByID(ctx, hostID)
	if err != nil {
		return store.Host{}, gitsync.Stack{}, err
	}
	for _, st := range s.repo.StacksForHost(host.Hostname) {
		if st.Name == stackName {
			return host, st, nil
		}
	}
	return host, gitsync.Stack{}, fmt.Errorf("stack %q ist für host %q nicht im repo beschrieben",
		stackName, host.Hostname)
}

// revertStack bringt den Host auf den Stand des Repos.
func (s *Server) revertStack(w http.ResponseWriter, r *http.Request, hostID, stackName string) {
	host, st, err := s.findStack(r.Context(), hostID, stackName)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	data, err := gitsync.ReadCompose(s.repo.workDir, st.ComposePath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	id := identityFrom(r.Context())
	res, err := s.applyStack(r.Context(), hostID, st.Name, data)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), HostID: hostID, Kind: "drift.revert", Actor: id.Actor(),
		Summary: fmt.Sprintf("Stack %q auf %s auf den Stand des Repos gebracht (%s)",
			st.Name, host.Hostname, res.Status),
	})

	code := http.StatusOK
	if res.Status == transport.StatusFailed {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, res)
}

// applyStack schickt die Compose-Datei an den Agenten und lässt sie anwenden.
func (s *Server) applyStack(ctx context.Context, hostID, stack string, composeYAML []byte) (transport.CmdResult, error) {
	// Großzügiges Zeitlimit: Ein `up` kann Images ziehen (ADR-0027).
	cmdCtx, cancel := context.WithTimeout(ctx, 16*time.Minute)
	defer cancel()

	res, err := s.hub.Execute(cmdCtx, hostID, transport.CmdRequest{
		CmdID:       fmt.Sprintf("%s:%s:up:%d", hostID, stack, time.Now().UnixNano()),
		Action:      transport.ActionStackUp,
		Stack:       stack,
		ComposeYAML: string(composeYAML),
		Deadline:    time.Now().Add(15 * time.Minute),
	})
	switch {
	case errors.Is(err, transport.ErrNotConnected):
		return res, errors.New("host ist nicht verbunden")
	case errors.Is(err, transport.ErrNotApprovedYet):
		return res, errors.New("host ist noch nicht bestätigt")
	case err != nil:
		return res, err
	}
	return res, nil
}

// adoptChange übernimmt den Zustand des Hosts ins Repo.
//
// Nur für geänderte Image-Angaben (ADR-0028): Alles andere erfordert
// strukturelle Eingriffe in eine Datei, die einem Menschen gehört — dort ist
// der Unterschied zwischen „richtig geändert" und „beschädigt" nicht mehr
// zuverlässig zu ziehen.
func (s *Server) adoptChange(w http.ResponseWriter, r *http.Request, hostID, stackName, service, field string) {
	if field != "image" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"adopt ist nur für geänderte image-angaben möglich (ADR-0028) — "+
				"die nötige Änderung steht in der Übersicht und muss von Hand eingetragen werden"))
		return
	}
	if service == "" {
		writeErr(w, http.StatusBadRequest, errors.New("service fehlt"))
		return
	}

	host, st, err := s.findStack(r.Context(), hostID, stackName)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	// Den aktuellen Vergleich neu berechnen statt dem Client zu glauben:
	// Der Wert, der ins Repo geschrieben wird, muss aus dem gemeldeten
	// Ist-Zustand stammen, nicht aus der Anfrage.
	view := s.compareStack(host, st)
	if view.Error != "" {
		writeErr(w, http.StatusConflict, errors.New(view.Error))
		return
	}
	var neuesImage string
	for _, d := range view.Drifts {
		if d.Service == service && d.Field == "image" {
			neuesImage = d.Actual
			break
		}
	}
	if neuesImage == "" {
		writeErr(w, http.StatusConflict,
			errors.New("für diesen dienst liegt keine image-abweichung (mehr) vor"))
		return
	}

	id := identityFrom(r.Context())
	commit, err := s.repo.AdoptImage(r.Context(), st.ComposePath, service, neuesImage, id.Username)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), HostID: hostID, Kind: "drift.adopt", Actor: id.Actor(),
		Summary: fmt.Sprintf("Image von %q in Stack %q ins Repo übernommen: %s",
			service, st.Name, neuesImage),
		Details: map[string]string{"commit": shortID(commit), "image": neuesImage},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "übernommen", "image": neuesImage, "commit": shortID(commit),
	})
}

// runApplyLoop gleicht Stacks im Modus `apply` an Git an.
//
// Der Modus ist opt-in pro Stack (ADR-0004) und die Vorgabe ist `observe`.
// Deshalb greift diese Schleife nur dort, wo der Nutzer sie ausdrücklich
// eingeschaltet hat.
func (s *Server) runApplyLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.applyOnce(ctx)
		}
	}
}

func (s *Server) applyOnce(ctx context.Context) {
	hosts, err := s.store.Hosts(ctx)
	if err != nil {
		return
	}
	for _, h := range hosts {
		if !h.Approved {
			continue
		}
		if _, connected := s.hub.Session(h.ID); !connected {
			continue
		}
		for _, st := range s.repo.StacksForHost(h.Hostname) {
			if st.Mode != gitsync.ModeApply {
				continue
			}
			view := s.compareStack(h, st)
			if view.InSync || view.Error != "" {
				continue
			}

			data, err := gitsync.ReadCompose(s.repo.workDir, st.ComposePath)
			if err != nil {
				continue
			}
			res, err := s.applyStack(ctx, h.ID, st.Name, data)
			if err != nil {
				s.logger.Warn("apply fehlgeschlagen",
					"host", h.Hostname, "stack", st.Name, "fehler", err)
				continue
			}
			if res.Status == transport.StatusOK {
				_ = s.store.AppendEvent(ctx, store.Event{
					At: time.Now().UTC(), HostID: h.ID, Kind: "drift.applied", Actor: "system",
					Summary: fmt.Sprintf("Stack %q auf %s automatisch angeglichen (Modus apply)",
						st.Name, h.Hostname),
					Details: map[string]string{"abweichungen": fmt.Sprint(len(view.Drifts))},
				})
				s.logger.Info("stack automatisch angeglichen",
					"host", h.Hostname, "stack", st.Name)
			}
		}
	}
}
