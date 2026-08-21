package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/transport"
)

// SourceURL verweist auf den Quelltext. Pflichtangabe nach AGPL §13.
//
// Zeigt bewusst auf das Projekt und nicht auf einen festen Commit: Eine
// abgewandelte Fassung muss diese Adresse auf den eigenen Quelltext ändern.
const SourceURL = "https://github.com/aronk11/havenry"

// Server bündelt Hub, Enroller, Zustandsspeicher, Auth und HTTP-Routen.
type Server struct {
	store   store.Full
	enroll  *Enroller
	hub     *transport.Hub
	state   *stateCache
	auth    *authService
	repo    *repoManager
	limiter *loginLimiter
	cors    *corsConfig
	logger  *slog.Logger
	version string

	// logSubs verteilt eingehende Log-Abschnitte an wartende HTTP-Streams.
	logMu   sync.Mutex
	logSubs map[string]chan transport.LogChunk
}

func NewServer(s store.Full, version, workDir string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	enr := NewEnroller(s, logger)
	hub := transport.NewHub(enr, logger)

	srv := &Server{
		store: s, enroll: enr, hub: hub, state: newStateCache(),
		auth:    newAuthService(s, logger),
		repo:    newRepoManager(s, workDir, logger),
		limiter: newLoginLimiter(),
		cors:    newCORS(nil),
		logger:  logger, version: version,
		logSubs: make(map[string]chan transport.LogChunk),
	}

	hub.OnState = func(hostID string, r transport.StateReport) {
		srv.state.putState(hostID, r)
		_ = s.TouchHost(context.Background(), hostID, time.Now().UTC())
	}
	hub.OnMetrics = func(hostID string, m transport.MetricsReport) {
		srv.state.putMetrics(hostID, m)
		_ = s.TouchHost(context.Background(), hostID, time.Now().UTC())
	}
	hub.OnLogChunk = func(_ string, c transport.LogChunk) {
		srv.deliverLogChunk(c)
	}
	hub.OnDisconnect = func(hostID string) {
		// Nur vergessen, wenn wirklich niemand mehr verbunden ist. Bei einer
		// Neuverbindung räumt die alte Sitzung verzögert auf — ohne diese
		// Prüfung würde sie den frischen Zustand der neuen Sitzung löschen,
		// und die Oberfläche zeigte grundlos "noch keine Zustandsmeldung".
		if _, stillConnected := hub.Session(hostID); stillConnected {
			return
		}
		srv.state.forget(hostID)
	}
	return srv
}

// Handler baut den HTTP-Router.
//
// Der Aufbau folgt ADR-0030: Jede API-Version registriert ihre Routen in einer
// eigenen Funktion. Kommt eines Tages v2 dazu, steht hier eine zweite Zeile —
// und beide Bäume laufen nebeneinander, bis v1 zurückgezogen wird.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Agenten authentifizieren sich über ihr eigenes Credential (ADR-0015)
	// und sprechen ein eigenes, getrennt versioniertes Protokoll (ADR-0016).
	mux.Handle("/agent", s.hub)

	// Unversioniert und offen: beide verraten nichts und werden gebraucht,
	// bevor ein Client weiß, ob er passt.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
	})
	mux.HandleFunc("GET /api/versions", s.listAPIVersions)

	// AGPL §13: Wer die Oberfläche über ein Netz benutzt, muss an den
	// Quelltext der laufenden Fassung kommen.
	mux.HandleFunc("GET /source", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, SourceURL, http.StatusFound)
	})

	registerV1(mux, s)

	mux.Handle("/", webUI())

	var h http.Handler = mux
	if s.cors.enabled() {
		// Nur umhüllen, wenn tatsächlich Herkünfte eingetragen sind — sonst
		// stünde eine Schicht im Weg, die nie etwas tut.
		h = s.cors.wrap(h)
	}
	return withAPIVersionHeader(APIVersion, h)
}

// SetAllowedOrigins erlaubt getrennt ausgelieferten Oberflächen den Zugriff
// (ADR-0032). Vor Handler() aufzurufen.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.cors = newCORS(origins)
	if s.cors.enabled() {
		s.logger.Info("cors aktiv", "herkuenfte", origins)
	}
}

// Start bereitet den Server vor: erster Admin, Repo wiederherstellen,
// Hintergrundschleifen.
func (s *Server) Start(ctx context.Context) error {
	if err := s.auth.EnsureInitialAdmin(ctx); err != nil {
		return err
	}
	if err := s.repo.Restore(ctx); err != nil {
		s.logger.Warn("repo-konfiguration konnte nicht geladen werden", "fehler", err)
	}
	go s.repo.Run(ctx)
	go s.purgeSessions(ctx)
	go s.runApplyLoop(ctx)
	return nil
}

// purgeSessions räumt abgelaufene Sitzungen weg.
func (s *Server) purgeSessions(ctx context.Context) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.store.PurgeExpiredSessions(ctx, time.Now().UTC())
		}
	}
}

type hostView struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	Approved     bool      `json:"approved"`
	Connected    bool      `json:"connected"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	AgentVersion string    `json:"agent_version"`
	EnrolledAt   time.Time `json:"enrolled_at"`
	LastSeen     time.Time `json:"last_seen"`
	Containers   int       `json:"containers"`
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprint(err)})
}
