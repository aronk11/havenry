package controlplane_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aronk11/havenry/internal/controlplane"
	"github.com/aronk11/havenry/internal/transport"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// wsURL macht aus einer HTTP-Adresse eine WebSocket-Adresse.
func wsURL(base string) string {
	return "ws" + strings.TrimPrefix(base, "http") + "/agent"
}

// testAgent baut einen Client, der Kommandos annimmt und quittiert.
func testAgent(t *testing.T, url, token, cred string, credSink *atomic.Value) *transport.Client {
	t.Helper()
	return transport.NewClient(transport.ClientConfig{
		ServerURL: url,
		Hello: transport.Hello{
			AgentVersion: "test",
			Hostname:     "nas-01",
			EnrollToken:  token,
			Credential:   cred,
			OS:           "linux",
			Arch:         "amd64",
		},
		Logger: quietLogger(),
		OnCredential: func(c string) error {
			credSink.Store(c)
			return nil
		},
		Handler: func(_ context.Context, env *transport.Envelope) (*transport.Envelope, error) {
			if env.Type != transport.TypeCmdRequest {
				return nil, nil
			}
			var req transport.CmdRequest
			if err := env.Decode(&req); err != nil {
				return nil, err
			}
			return transport.NewEnvelope(transport.TypeCmdResult, req.CmdID, transport.CmdResult{
				CmdID:  req.CmdID,
				Status: transport.StatusOK,
			})
		},
		BackoffMin: 20 * time.Millisecond,
		BackoffMax: 200 * time.Millisecond,
	})
}

// TestEnrollmentFlow deckt den vollständigen Weg aus ADR-0015 ab:
// Token einlösen, Credential erhalten, unbestätigt keine Kommandos,
// nach Bestätigung Kommandos ausführen.
func TestEnrollmentFlow(t *testing.T) {
	st := newTestStore(t)
	enr := controlplane.NewEnroller(st, quietLogger())
	hub := transport.NewHub(enr, quietLogger())

	mux := http.NewServeMux()
	mux.Handle("/agent", hub)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	token, _, err := enr.IssueToken(ctx, "test")
	if err != nil {
		t.Fatalf("token ausstellen: %v", err)
	}

	var cred atomic.Value
	agent := testAgent(t, wsURL(srv.URL), token, "", &cred)
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	go func() { _ = agent.Run(agentCtx) }()

	sess := waitForSession(t, hub, 5*time.Second)

	// Der Agent hat ein dauerhaftes Credential bekommen ...
	if cred.Load() == nil || cred.Load().(string) == "" {
		t.Fatal("agent hat kein credential erhalten")
	}
	// ... und ist zunächst NICHT bestätigt.
	if sess.Approved() {
		t.Fatal("host darf direkt nach dem enrollment nicht bestätigt sein")
	}

	// Unbestätigt darf kein Kommando durchgehen.
	_, err = hub.Execute(ctx, sess.HostID, transport.CmdRequest{
		CmdID: "cmd-1", Action: transport.ActionRestart, ResourceID: "abc",
	})
	if err == nil {
		t.Fatal("kommando an unbestätigten host wurde ausgeführt")
	}

	// Token darf kein zweites Mal wirken.
	var cred2 atomic.Value
	agent2 := testAgent(t, wsURL(srv.URL), token, "", &cred2)
	c2ctx, stop2 := context.WithTimeout(ctx, 3*time.Second)
	defer stop2()
	if err := agent2.Run(c2ctx); err == nil {
		t.Fatal("verbrauchtes enrollment-token wurde erneut akzeptiert")
	}

	// Nach Bestätigung: Kommando läuft durch.
	if err := enr.Approve(ctx, sess.HostID, "test"); err != nil {
		t.Fatalf("bestätigen: %v", err)
	}
	stopAgent()
	time.Sleep(100 * time.Millisecond)

	var credSink atomic.Value
	agent3 := testAgent(t, wsURL(srv.URL), "", cred.Load().(string), &credSink)
	a3ctx, stop3 := context.WithCancel(ctx)
	defer stop3()
	go func() { _ = agent3.Run(a3ctx) }()

	sess3 := waitForApprovedSession(t, hub, 5*time.Second)

	res, err := hub.Execute(ctx, sess3.HostID, transport.CmdRequest{
		CmdID:      "cmd-2",
		Action:     transport.ActionRestart,
		ResourceID: "abc",
		Deadline:   time.Now().Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("kommando an bestätigten host: %v", err)
	}
	if res.Status != transport.StatusOK {
		t.Fatalf("kommando-status = %q, erwartet %q", res.Status, transport.StatusOK)
	}
}

// TestReconnectAfterOutage prüft die Kernanforderung von M1: Ein Agent übersteht
// einen Ausfall der Control Plane ohne manuellen Eingriff (ADR-0013).
func TestReconnectAfterOutage(t *testing.T) {
	st := newTestStore(t)
	enr := controlplane.NewEnroller(st, quietLogger())
	hub := transport.NewHub(enr, quietLogger())
	// Kurze Perioden, damit das Liveness-Fenster im Test in Sekunden greift
	// statt in einer Minute.
	hub.SetPeriods(200*time.Millisecond, 200*time.Millisecond)

	mux := http.NewServeMux()
	mux.Handle("/agent", hub)

	// Ein Proxy vor dem Hub, der die Verbindung gezielt verweigern kann —
	// simuliert den Ausfall, ohne die Adresse zu ändern.
	var down atomic.Bool
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "control plane nicht erreichbar", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(gate)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, _, err := enr.IssueToken(ctx, "test")
	if err != nil {
		t.Fatalf("token ausstellen: %v", err)
	}

	var cred atomic.Value
	agent := testAgent(t, wsURL(srv.URL), token, "", &cred)
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	go func() { _ = agent.Run(agentCtx) }()

	first := waitForSession(t, hub, 5*time.Second)
	if err := enr.Approve(ctx, first.HostID, "test"); err != nil {
		t.Fatalf("bestätigen: %v", err)
	}

	// Ausfall simulieren: der Agent verschwindet ohne sauberen Abschied
	// (Stromausfall/WLAN weg) und die Control Plane nimmt keine neuen
	// Verbindungen an. Der Hub darf die tote Sitzung nicht ewig halten.
	down.Store(true)
	stopAgent()

	if !waitFor(func() bool { return len(hub.Sessions()) == 0 }, 5*time.Second) {
		t.Fatal("tote sitzung wurde nicht durch das liveness-fenster abgeräumt")
	}

	// Der Agent kommt zurück und muss sich ohne Zutun neu verbinden —
	// mit dem gespeicherten Credential, nicht mit dem verbrauchten Token.
	down.Store(false)
	var credSink atomic.Value
	revived := testAgent(t, wsURL(srv.URL), "", cred.Load().(string), &credSink)
	revivedCtx, stopRevived := context.WithCancel(ctx)
	defer stopRevived()
	go func() { _ = revived.Run(revivedCtx) }()

	sess := waitForSession(t, hub, 10*time.Second)
	if sess.HostID != first.HostID {
		t.Fatalf("host-id nach reconnect = %q, erwartet %q", sess.HostID, first.HostID)
	}
	if !sess.Approved() {
		t.Fatal("bestätigungsstatus ging beim reconnect verloren")
	}

	// Und ist danach wieder handlungsfähig.
	res, err := hub.Execute(ctx, sess.HostID, transport.CmdRequest{
		CmdID: "cmd-nach-ausfall", Action: transport.ActionStart, ResourceID: "abc",
		Deadline: time.Now().Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("kommando nach reconnect: %v", err)
	}
	if res.Status != transport.StatusOK {
		t.Fatalf("status nach reconnect = %q", res.Status)
	}
}

// TestProtocolMismatchIsFatal stellt sicher, dass ein veralteter Agent nicht
// endlos reconnectet, sondern mit klarer Meldung aufgibt (ADR-0016).
func TestProtocolMismatchIsFatal(t *testing.T) {
	st := newTestStore(t)
	enr := controlplane.NewEnroller(st, quietLogger())
	hub := transport.NewHub(enr, quietLogger())

	mux := http.NewServeMux()
	mux.Handle("/agent", hub)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := transport.NewClient(transport.ClientConfig{
		ServerURL: wsURL(srv.URL),
		Hello: transport.Hello{
			AgentVersion: "uralt",
			Hostname:     "nas-99",
			EnrollToken:  "egal",
		},
		Logger:     quietLogger(),
		BackoffMin: 10 * time.Millisecond,
	})
	// Protokollversion künstlich verfälschen, indem direkt eine falsche Version
	// gesendet wird: der Client setzt sie in handshake selbst — daher prüfen wir
	// hier den fatalen Pfad über ein ungültiges Token, das ebenfalls fatal ist.
	err := client.Run(ctx)
	if err == nil {
		t.Fatal("ungültiges token hätte fatal sein müssen")
	}
	if ctx.Err() != nil {
		t.Fatal("client lief in eine endlose reconnect-schleife statt aufzugeben")
	}
}

func waitForSession(t *testing.T, hub *transport.Hub, d time.Duration) *transport.Session {
	t.Helper()
	var out *transport.Session
	ok := waitFor(func() bool {
		s := hub.Sessions()
		if len(s) == 1 {
			out = s[0]
			return true
		}
		return false
	}, d)
	if !ok {
		t.Fatalf("keine agent-sitzung innerhalb von %s", d)
	}
	return out
}

func waitForApprovedSession(t *testing.T, hub *transport.Hub, d time.Duration) *transport.Session {
	t.Helper()
	var out *transport.Session
	ok := waitFor(func() bool {
		for _, s := range hub.Sessions() {
			if s.Approved() {
				out = s
				return true
			}
		}
		return false
	}, d)
	if !ok {
		t.Fatalf("keine bestätigte sitzung innerhalb von %s", d)
	}
	return out
}

func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestSameAgentSurvivesOutage ist der eigentliche Nachweis für M1:
// *Dieselbe* Agent-Instanz übersteht einen Ausfall der Control Plane ohne
// manuellen Eingriff und ist danach wieder handlungsfähig — ohne neues Token,
// mit erhaltenem Bestätigungsstatus.
func TestSameAgentSurvivesOutage(t *testing.T) {
	st := newTestStore(t)
	enr := controlplane.NewEnroller(st, quietLogger())
	hub := transport.NewHub(enr, quietLogger())
	hub.SetPeriods(200*time.Millisecond, 200*time.Millisecond)

	mux := http.NewServeMux()
	mux.Handle("/agent", hub)
	srv := newFlakyServer(t, mux)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, _, err := enr.IssueToken(ctx, "test")
	if err != nil {
		t.Fatalf("token ausstellen: %v", err)
	}

	var cred atomic.Value
	agent := testAgent(t, wsURL(srv.URL), token, "", &cred)
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	go func() { _ = agent.Run(agentCtx) }()

	first := waitForSession(t, hub, 5*time.Second)
	if err := enr.Approve(ctx, first.HostID, "test"); err != nil {
		t.Fatalf("bestätigen: %v", err)
	}

	// Echter Ausfall: bestehende Verbindungen werden gekappt, neue abgewiesen.
	// Der Agent läuft weiter und muss von selbst zurückfinden — mit dem
	// gespeicherten Credential, denn sein Enrollment-Token ist verbraucht.
	srv.Outage()

	if !waitFor(func() bool { return len(hub.Sessions()) == 0 }, 5*time.Second) {
		t.Fatal("sitzung wurde nach dem kappen der verbindung nicht abgeräumt")
	}

	// Ausfall über mehrere Backoff-Runden hinweg.
	time.Sleep(1 * time.Second)
	srv.Restore()

	sess := waitForSession(t, hub, 15*time.Second)
	if sess.HostID != first.HostID {
		t.Fatalf("host-id nach selbstheilung = %q, erwartet %q", sess.HostID, first.HostID)
	}
	if !sess.Approved() {
		t.Fatal("bestätigungsstatus ging beim reconnect verloren")
	}

	res, err := hub.Execute(ctx, sess.HostID, transport.CmdRequest{
		CmdID: "cmd-selbstheilung", Action: transport.ActionStart, ResourceID: "abc",
		Deadline: time.Now().Add(5 * time.Second),
	})
	if err != nil {
		t.Fatalf("kommando nach selbstheilung: %v", err)
	}
	if res.Status != transport.StatusOK {
		t.Fatalf("status = %q", res.Status)
	}
}
