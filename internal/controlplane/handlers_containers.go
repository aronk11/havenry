package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aronk11/havenry/internal/provider/docker"
	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/transport"
)

// Container-Sicht und -Steuerung.
//
// Die Gruppierung nach Compose-Projekt passiert hier und nicht im Agenten:
// Der Agent meldet rohen Zustand, die Deutung gehört in die Control Plane.

// collect baut die Container-Sicht über alle Hosts.
func (s *Server) collect(ctx context.Context) ([]containerView, map[string]string, error) {
	hosts, err := s.store.Hosts(ctx)
	if err != nil {
		return nil, nil, err
	}
	hosts = visibleHosts(identityFrom(ctx), hosts)

	names := make(map[string]string, len(hosts))
	for _, h := range hosts {
		names[h.ID] = h.Hostname
	}

	var out []containerView
	for _, h := range hosts {
		st, ok := s.state.get(h.ID)
		if !ok {
			continue
		}
		for _, r := range st.Resources {
			out = append(out, containerView{
				ID: r.ID, HostID: h.ID, HostName: h.Hostname,
				Name: r.Name, Stack: r.Stack,
				Service: r.Labels[docker.LabelService],
				Image:   r.Image, State: r.State, Health: r.Health,
				Restarts: r.Restarts, Ports: r.Ports,
			})
		}
	}
	return out, names, nil
}

func (s *Server) listContainers(w http.ResponseWriter, r *http.Request) {
	containers, _, err := s.collect(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
}

// listStacks gruppiert nach Compose-Projekt. Container ohne Stack-Label
// landen in einer Sammelgruppe — sie verschwinden nicht, denn ein per Hand
// gestarteter Container ist genau die Art Abweichung, die sichtbar sein soll.
func (s *Server) listStacks(w http.ResponseWriter, r *http.Request) {
	containers, _, err := s.collect(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	type key struct{ host, stack string }
	groups := map[key]*stackView{}

	for _, c := range containers {
		name := c.Stack
		if name == "" {
			name = "(ohne stack)"
		}
		k := key{c.HostID, name}
		g, ok := groups[k]
		if !ok {
			g = &stackView{Name: name, HostID: c.HostID, HostName: c.HostName}
			groups[k] = g
		}
		g.Containers = append(g.Containers, c)
		g.Total++
		if c.State == "running" {
			g.Running++
		}
	}

	out := make([]stackView, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sortStacks(out)
	writeJSON(w, http.StatusOK, map[string]any{"stacks": out})
}

// containerAction führt start/stop/restart aus.
func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	hostID, id, action := r.PathValue("hostID"), r.PathValue("id"), r.PathValue("action")

	if !s.requireHostAccess(w, r, hostID) {
		return
	}

	switch action {
	case transport.ActionStart, transport.ActionStop, transport.ActionRestart:
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("aktion %q nicht erlaubt", action))
		return
	}

	// Der Zeitstempel in der CmdID ist Absicht: Ein Nutzer, der zweimal auf
	// "Neustart" klickt, will zwei Neustarts — das ist keine Doppelzustellung.
	// Die Idempotenz-Zusage aus ADR-0013 gilt der Wiederholung *derselben*
	// Nachricht nach einem Verbindungsabbruch; die trägt dieselbe CmdID und
	// wird agentenseitig aus dem Ergebnisspeicher beantwortet.
	cmdID := fmt.Sprintf("%s:%s:%s:%d", hostID, id, action, time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	res, err := s.hub.Execute(ctx, hostID, transport.CmdRequest{
		CmdID: cmdID, Action: action, ResourceID: id,
		Deadline: time.Now().Add(60 * time.Second),
	})
	switch {
	case errors.Is(err, transport.ErrNotConnected):
		writeErr(w, http.StatusServiceUnavailable, errors.New("host ist nicht verbunden"))
		return
	case errors.Is(err, transport.ErrNotApprovedYet):
		writeErr(w, http.StatusForbidden, errors.New("host ist noch nicht bestätigt"))
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), HostID: hostID, Kind: "container." + action,
		Actor:   identityFrom(r.Context()).Actor(),
		Summary: fmt.Sprintf("%s auf Container %s: %s", action, shortID(id), res.Status),
		Details: map[string]string{"container_id": id, "status": res.Status, "message": res.Message},
	})

	code := http.StatusOK
	if res.Status == transport.StatusFailed {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, res)
}
