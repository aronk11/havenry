package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aronk11/havenry/internal/transport"
)

// Log-Streaming vom Agenten zum Browser.
//
// Der Weg ist: Agent -> WebSocket -> Control Plane -> Server-Sent Events ->
// Browser. Die Zustellung an den Browser ist bewusst nicht blockierend — ein
// langsamer Client darf nicht die Agent-Verbindung ausbremsen.

// containerLogs streamt Logs als Server-Sent Events.
func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	hostID, id := r.PathValue("hostID"), r.PathValue("id")

	if !s.requireHostAccess(w, r, hostID) {
		return
	}

	sess, ok := s.hub.Session(hostID)
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, errors.New("host ist nicht verbunden"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming nicht unterstützt"))
		return
	}

	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			tail = n
		}
	}

	subID := fmt.Sprintf("log-%d", time.Now().UnixNano())
	ch := make(chan transport.LogChunk, 256)

	s.logMu.Lock()
	s.logSubs[subID] = ch
	s.logMu.Unlock()
	defer func() {
		s.logMu.Lock()
		delete(s.logSubs, subID)
		s.logMu.Unlock()

		// Dem Agenten sagen, dass niemand mehr zuhört. Ohne das liefe dort
		// eine Goroutine samt offener Docker-Verbindung weiter.
		// Eigener Kontext: der des Requests ist an dieser Stelle schon beendet.
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sess.Send(stopCtx, transport.TypeLogUnsubscribe,
			transport.LogUnsubscribe{SubID: subID})
	}()

	if err := sess.Send(r.Context(), transport.TypeLogSubscribe, transport.LogSubscribe{
		SubID: subID, ResourceID: id, TailLines: tail, Follow: true,
	}); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case chunk := <-ch:
			if chunk.Data != "" {
				b, _ := json.Marshal(chunk.Data)
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()
			}
			if chunk.EOF {
				fmt.Fprint(w, "event: eof\ndata: \"\"\n\n")
				flusher.Flush()
				return
			}
		}
	}
}

// deliverLogChunk stellt einen Abschnitt zu.
//
// Bewusst nicht blockierend: Ein langsamer Browser darf nicht die
// Agent-Verbindung ausbremsen. Lieber ein paar Zeilen verlieren als den
// gesamten Nachrichtenkanal stauen.
func (s *Server) deliverLogChunk(c transport.LogChunk) {
	s.logMu.Lock()
	ch, ok := s.logSubs[c.SubID]
	s.logMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- c:
	default:
	}
}
