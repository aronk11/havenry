package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Authenticator prüft das Hello eines Agenten und entscheidet über die Aufnahme.
// Die Implementierung liegt in internal/controlplane (ADR-0015) — der Transport
// kennt keine Enrollment-Logik.
type Authenticator interface {
	// Authenticate prüft Token oder Credential und liefert die Antwort.
	// Ein Fehler mit ErrorPayload führt zur Ablehnung der Verbindung.
	Authenticate(ctx context.Context, h Hello) (HelloAck, error)
}

// AuthError ist ein Ablehnungsgrund mit Protokoll-Fehlercode.
type AuthError struct {
	Code    string
	Message string
}

func (e *AuthError) Error() string { return e.Code + ": " + e.Message }

// Session ist eine aktive Agent-Verbindung aus Sicht der Control Plane.
type Session struct {
	HostID   string
	Hostname string
	Since    time.Time
	// Capabilities meldet, was dieser Host kann — etwa "apply", wenn die
	// Compose-CLI vorhanden ist (ADR-0027).
	Capabilities []string

	conn *websocket.Conn
	hub  *Hub

	// approved wird zur Laufzeit gesetzt, wenn der Nutzer den Host in der
	// Oberfläche bestätigt — ohne dass der Agent sich neu verbinden muss.
	approved atomic.Bool

	mu      sync.Mutex
	pending map[string]chan CmdResult
}

// Approved meldet, ob der Host bestätigt ist (ADR-0015).
func (s *Session) Approved() bool { return s.approved.Load() }

// HasCapability meldet, ob der Agent eine Fähigkeit gemeldet hat.
func (s *Session) HasCapability(c string) bool {
	for _, have := range s.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// Hub verwaltet alle Agent-Verbindungen.
type Hub struct {
	auth   Authenticator
	logger *slog.Logger

	// OnState und OnMetrics werden bei eingehenden Reports aufgerufen.
	OnState   func(hostID string, r StateReport)
	OnMetrics func(hostID string, r MetricsReport)
	// OnLogChunk wird bei Log-Daten aufgerufen.
	OnLogChunk func(hostID string, c LogChunk)
	// OnDisconnect meldet das Ende einer Sitzung. Der Aufrufer nutzt das, um
	// den zwischengespeicherten Ist-Zustand zu verwerfen: Container-Daten von
	// einem getrennten Host sind Fehlinformation, keine Information.
	OnDisconnect func(hostID string)

	mu       sync.RWMutex
	sessions map[string]*Session

	heartbeatPeriod time.Duration
	reportPeriod    time.Duration
}

func NewHub(auth Authenticator, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		auth:            auth,
		logger:          logger,
		sessions:        make(map[string]*Session),
		heartbeatPeriod: 20 * time.Second,
		reportPeriod:    15 * time.Second,
	}
}

// SetPeriods überschreibt Heartbeat- und Report-Intervall. Wird für Tests und
// für langsame Verbindungen gebraucht; das Liveness-Fenster leitet sich daraus ab.
func (h *Hub) SetPeriods(heartbeat, report time.Duration) {
	if heartbeat > 0 {
		h.heartbeatPeriod = heartbeat
	}
	if report > 0 {
		h.reportPeriod = report
	}
}

// ServeHTTP nimmt Agent-Verbindungen entgegen. Wird unter /agent gemountet.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Agenten sind keine Browser; Origin-Prüfung entfällt bewusst.
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error("verbindung annehmen", "fehler", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck

	conn.SetReadLimit(8 << 20)
	ctx := r.Context()

	sess, err := h.accept(ctx, conn)
	if err != nil {
		h.logger.Warn("agent abgelehnt", "fehler", err, "remote", r.RemoteAddr)
		return
	}

	h.logger.Info("agent verbunden",
		"host_id", sess.HostID, "hostname", sess.Hostname, "bestaetigt", sess.Approved())

	h.register(sess)
	defer func() {
		h.unregister(sess)
		if h.OnDisconnect != nil {
			h.OnDisconnect(sess.HostID)
		}
	}()

	if err := h.serve(ctx, sess); err != nil && !errors.Is(err, context.Canceled) {
		h.logger.Info("agent getrennt", "host_id", sess.HostID, "grund", err)
	}
}

// accept führt den Handshake durch.
func (h *Hub) accept(ctx context.Context, conn *websocket.Conn) (*Session, error) {
	hsCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	env, err := readEnvelope(hsCtx, conn)
	if err != nil {
		return nil, fmt.Errorf("hello empfangen: %w", err)
	}
	if env.Type != TypeHello {
		return nil, fmt.Errorf("erste nachricht war %q, erwartet %q", env.Type, TypeHello)
	}

	var hello Hello
	if err := env.Decode(&hello); err != nil {
		return nil, err
	}

	if hello.ProtocolVersion != ProtocolVersion {
		h.reject(hsCtx, conn, ErrProtocolMismatch,
			fmt.Sprintf("agent spricht protokoll v%d, control plane erwartet v%d — bitte agent aktualisieren",
				hello.ProtocolVersion, ProtocolVersion))
		return nil, fmt.Errorf("protokollversion %d", hello.ProtocolVersion)
	}

	ack, err := h.auth.Authenticate(hsCtx, hello)
	if err != nil {
		var ae *AuthError
		if errors.As(err, &ae) {
			h.reject(hsCtx, conn, ae.Code, ae.Message)
		} else {
			h.reject(hsCtx, conn, ErrInternal, "interner fehler")
		}
		return nil, err
	}

	ack.HeartbeatPeriod = h.heartbeatPeriod
	ack.ReportPeriod = h.reportPeriod

	resp, err := NewEnvelope(TypeHelloAck, env.ID, ack)
	if err != nil {
		return nil, err
	}
	if err := writeEnvelope(hsCtx, conn, resp); err != nil {
		return nil, fmt.Errorf("hello.ack senden: %w", err)
	}

	sess := &Session{
		HostID:       ack.HostID,
		Hostname:     hello.Hostname,
		Capabilities: hello.Capabilities,
		Since:        time.Now(),
		conn:         conn,
		hub:          h,
		pending:      make(map[string]chan CmdResult),
	}
	sess.approved.Store(ack.Approved)
	return sess, nil
}

func (h *Hub) reject(ctx context.Context, conn *websocket.Conn, code, msg string) {
	env, err := NewEnvelope(TypeError, "", ErrorPayload{Code: code, Message: msg})
	if err != nil {
		return
	}
	_ = writeEnvelope(ctx, conn, env)
}

func (h *Hub) register(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Ein Host kann sich neu verbinden, bevor die alte Sitzung aufgeräumt ist.
	// Die neue Verbindung gewinnt.
	if old, ok := h.sessions[s.HostID]; ok {
		_ = old.conn.Close(websocket.StatusNormalClosure, "durch neue verbindung ersetzt")
	}
	h.sessions[s.HostID] = s
}

func (h *Hub) unregister(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.sessions[s.HostID]; ok && cur == s {
		delete(h.sessions, s.HostID)
	}
}

// serve liest Nachrichten der Sitzung, bis die Verbindung endet.
//
// Wichtig: Ein Agent kann verschwinden, ohne die Verbindung sauber zu schließen —
// Stromausfall, gekapptes WLAN, eingeschlafener Host. TCP merkt das unter
// Umständen minutenlang nicht. Deshalb gilt ein hartes Aktivitätsfenster:
// bleibt länger als das Dreifache der Heartbeat-Periode jede Nachricht aus,
// gilt die Sitzung als tot und wird abgeräumt. Sonst zeigt die Oberfläche
// Hosts als verbunden an, die es längst nicht mehr sind.
func (h *Hub) serve(ctx context.Context, s *Session) error {
	idleTimeout := 3 * h.heartbeatPeriod

	for {
		readCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		env, err := readEnvelope(readCtx, s.conn)
		cancel()
		if err != nil {
			if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				_ = s.conn.Close(websocket.StatusGoingAway, "keine aktivität")
				return fmt.Errorf("keine aktivität seit %s", idleTimeout)
			}
			return err
		}

		switch env.Type {
		case TypePing:
			pong, _ := NewEnvelope(TypePong, env.ID, nil)
			if err := writeEnvelope(ctx, s.conn, pong); err != nil {
				return err
			}

		case TypePong:
			// Heartbeat-Antwort, nichts zu tun.

		case TypeReportState:
			var r StateReport
			if err := env.Decode(&r); err != nil {
				h.logger.Warn("state-report dekodieren", "host_id", s.HostID, "fehler", err)
				continue
			}
			if h.OnState != nil {
				h.OnState(s.HostID, r)
			}

		case TypeReportMetrics:
			var r MetricsReport
			if err := env.Decode(&r); err != nil {
				continue
			}
			if h.OnMetrics != nil {
				h.OnMetrics(s.HostID, r)
			}

		case TypeCmdResult:
			var res CmdResult
			if err := env.Decode(&res); err != nil {
				continue
			}
			s.deliver(res)

		case TypeLogChunk:
			var c LogChunk
			if err := env.Decode(&c); err != nil {
				continue
			}
			if h.OnLogChunk != nil {
				h.OnLogChunk(s.HostID, c)
			}

		default:
			h.logger.Warn("unbekannter nachrichtentyp", "typ", env.Type, "host_id", s.HostID)
		}
	}
}

// Session liefert die aktive Sitzung eines Hosts.
func (h *Hub) Session(hostID string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[hostID]
	return s, ok
}

// SetApproved setzt den Bestätigungsstatus einer laufenden Sitzung.
// Ohne das müsste der Nutzer den Agenten nach der Freigabe neu starten.
func (h *Hub) SetApproved(hostID string, approved bool) {
	if s, ok := h.Session(hostID); ok {
		s.approved.Store(approved)
	}
}

// Sessions liefert alle aktiven Sitzungen.
func (h *Hub) Sessions() []*Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s)
	}
	return out
}

// ErrNotConnected wird geliefert, wenn ein Host gerade nicht verbunden ist.
var ErrNotConnected = errors.New("host nicht verbunden")

// ErrNotApprovedYet wird geliefert, wenn der Host in der Oberfläche noch nicht
// bestätigt wurde (ADR-0015).
var ErrNotApprovedYet = errors.New("host noch nicht bestätigt")

// Execute schickt ein Kommando und wartet auf das Ergebnis.
//
// Der Aufrufer ist dafür verantwortlich, dass die Aktion idempotent ist:
// Bei einem Verbindungsabbruch zwischen Ausführung und Ergebnis kann dasselbe
// Kommando erneut gestellt werden (ADR-0013).
func (h *Hub) Execute(ctx context.Context, hostID string, req CmdRequest) (CmdResult, error) {
	s, ok := h.Session(hostID)
	if !ok {
		return CmdResult{}, ErrNotConnected
	}
	if !s.Approved() {
		return CmdResult{}, ErrNotApprovedYet
	}
	return s.execute(ctx, req)
}

func (s *Session) execute(ctx context.Context, req CmdRequest) (CmdResult, error) {
	if req.CmdID == "" {
		return CmdResult{}, errors.New("cmd_id fehlt")
	}
	if req.Deadline.IsZero() {
		req.Deadline = time.Now().Add(2 * time.Minute)
	}

	ch := make(chan CmdResult, 1)
	s.mu.Lock()
	s.pending[req.CmdID] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, req.CmdID)
		s.mu.Unlock()
	}()

	env, err := NewEnvelope(TypeCmdRequest, req.CmdID, req)
	if err != nil {
		return CmdResult{}, err
	}
	if err := writeEnvelope(ctx, s.conn, env); err != nil {
		return CmdResult{}, fmt.Errorf("kommando senden: %w", err)
	}

	// Jedes Kommando erreicht einen Endzustand — notfalls durch Timeout.
	waitCtx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()

	select {
	case res := <-ch:
		return res, nil
	case <-waitCtx.Done():
		return CmdResult{
			CmdID:   req.CmdID,
			Status:  StatusFailed,
			Message: "zeitüberschreitung — ergebnis unbekannt, kommando kann erneut gestellt werden",
		}, nil
	}
}

func (s *Session) deliver(res CmdResult) {
	s.mu.Lock()
	ch, ok := s.pending[res.CmdID]
	s.mu.Unlock()
	if ok {
		select {
		case ch <- res:
		default:
		}
	}
}

// Send verschickt eine Nachricht an diesen Agenten.
func (s *Session) Send(ctx context.Context, typ string, payload any) error {
	env, err := NewEnvelope(typ, "", payload)
	if err != nil {
		return err
	}
	return writeEnvelope(ctx, s.conn, env)
}
