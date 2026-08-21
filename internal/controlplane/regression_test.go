package controlplane_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aronk11/havenry/internal/controlplane"
	"github.com/aronk11/havenry/internal/gitsync"
	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/transport"
)

// Diese Datei hält Fehler fest, die beim Feinschliff gefunden wurden. Ohne
// Test kommen sie beim nächsten Umbau zurück.

// TestReconnectKeepsReportedState hält Fund B fest.
//
// Beim Verbindungswechsel räumt die alte Sitzung verzögert auf. Ohne Prüfung,
// ob inzwischen jemand anders verbunden ist, löschte dieses Aufräumen den
// frisch gemeldeten Zustand der NEUEN Sitzung — die Oberfläche zeigte dann
// grundlos "noch keine Zustandsmeldung", obwohl der Host verbunden war.
//
// Der Test läuft bewusst gegen den echten Server und prüft das Ergebnis über
// die API. Eine im Test nachgebaute Aufräumlogik hätte nur sich selbst
// geprüft — genau dieser Fehler ist bei der ersten Fassung passiert.
func TestReconnectKeepsReportedState(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	c := &apiClient{t: t, base: ts.URL}
	c.login("admin", "admin-passwort-lang-genug")

	_, out := c.do("POST", "/api/v1/enroll-tokens", nil)
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("kein enrollment-token erhalten")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cred atomic.Value
	first := reportingAgent(t, wsAgentURL(ts.URL), token, "", &cred)
	firstCtx, stopFirst := context.WithCancel(ctx)
	defer stopFirst()
	go func() { _ = first.Run(firstCtx) }()

	// Warten, bis der Host da und bestätigt ist.
	hostID := waitForHostID(t, c, 5*time.Second)
	c.do("POST", "/api/v1/hosts/"+hostID+"/approve", nil)

	if !waitForContainers(t, c, 1, 5*time.Second) {
		t.Fatal("der gemeldete zustand kam nicht in der api an")
	}

	// Zweite Verbindung desselben Hosts, während die erste noch steht.
	var credSink atomic.Value
	second := reportingAgent(t, wsAgentURL(ts.URL), "", cred.Load().(string), &credSink)
	secondCtx, stopSecond := context.WithCancel(ctx)
	defer stopSecond()
	go func() { _ = second.Run(secondCtx) }()

	// Genug Zeit, dass die alte Sitzung ihr Aufräumen ausführt.
	time.Sleep(2 * time.Second)

	_, res := c.do("GET", "/api/v1/containers", nil)
	containers, _ := res["containers"].([]any)
	if len(containers) == 0 {
		t.Fatal("nach der neuverbindung ist der gemeldete zustand verschwunden — " +
			"das aufräumen der alten sitzung hat den der neuen gelöscht")
	}
}

// TestPushCheckIsCached hält Fund D fest.
//
// Die Drift-Ansicht wird alle fünf Sekunden abgerufen. Ohne Zwischenspeicher
// wäre die Schreibrechtsprüfung alle fünf Sekunden ein `git push --dry-run`
// gegen die Gegenstelle — pro angemeldetem Nutzer.
//
// Nachgewiesen über den gemeldeten Prüfzeitpunkt: Bleibt er über viele Abrufe
// gleich, wurde zwischengespeichert. Eine Zeitmessung taugt nicht — gegen ein
// lokales Repo sind zwanzig Prüfungen in unter hundert Millisekunden erledigt.
func TestPushCheckIsCached(t *testing.T) {
	if _, err := gitsync.CheckGitAvailable(); err != nil {
		t.Skip("git nicht verfügbar")
	}

	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	c := &apiClient{t: t, base: ts.URL}
	c.login("admin", "admin-passwort-lang-genug")

	// Ein Host mit passendem Namen, sonst gibt es keinen Stack zu vergleichen
	// und die Prüfung findet gar nicht erst statt.
	if err := st.UpsertHost(context.Background(), store.Host{
		ID: "host-1", Hostname: "nas-01", CredentialHash: "c1",
	}); err != nil {
		t.Fatal(err)
	}

	// Echtes Repo, damit die Prüfung überhaupt stattfindet.
	origin := newTestRepo(t)
	resp, out := c.do("PUT", "/api/v1/repo", map[string]any{"url": origin, "branch": "main"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repo setzen = %d: %v", resp.StatusCode, out)
	}

	first := adoptCheckedAt(t, c)
	if first == "" {
		t.Fatal("kein prüfzeitpunkt gemeldet — findet die prüfung statt?")
	}
	for i := 0; i < 20; i++ {
		if got := adoptCheckedAt(t, c); got != first {
			t.Fatalf("prüfzeitpunkt änderte sich bei abruf %d (%s → %s) — "+
				"die schreibrechtsprüfung läuft bei jedem abruf neu", i, first, got)
		}
	}
}

func adoptCheckedAt(t *testing.T, c *apiClient) string {
	t.Helper()
	_, out := c.do("GET", "/api/v1/drift", nil)
	items, _ := out["drift"].([]any)
	if len(items) == 0 {
		return ""
	}
	m, _ := items[0].(map[string]any)
	s, _ := m["adopt_checked_at"].(string)
	return s
}

// TestLogStreamStopsWhenClientLeaves hält Fund A fest.
//
// Schließt der Browser die Log-Ansicht, muss der Agent das erfahren. Ohne
// Abbestellung liefe dort eine Goroutine samt offener Docker-Verbindung
// unbegrenzt weiter — jeder Log-Aufruf hinterließe eine Leiche.
func TestLogStreamStopsWhenClientLeaves(t *testing.T) {
	st := newTestStore(t)
	enr := controlplaneEnroller(t, st)
	hub := transport.NewHub(enr, quietLogger())

	var unsubscribed atomic.Bool
	mux := http.NewServeMux()
	mux.Handle("/agent", hub)
	srv := newFlakyServer(t, mux)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, _, err := enr.IssueToken(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Agent, der Abbestellungen vermerkt.
	var cred atomic.Value
	client := transport.NewClient(transport.ClientConfig{
		ServerURL: wsURL(srv.URL),
		Hello: transport.Hello{
			AgentVersion: "test", Hostname: "nas-01",
			EnrollToken: token, OS: "linux", Arch: "amd64",
		},
		Logger:       quietLogger(),
		OnCredential: func(c string) error { cred.Store(c); return nil },
		Handler: func(_ context.Context, env *transport.Envelope) (*transport.Envelope, error) {
			if env.Type == transport.TypeLogUnsubscribe {
				unsubscribed.Store(true)
			}
			return nil, nil
		},
		BackoffMin: 20 * time.Millisecond,
	})
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	go func() { _ = client.Run(agentCtx) }()

	sess := waitForSession(t, hub, 5*time.Second)

	// Abbestellung schicken und prüfen, dass sie ankommt.
	if err := sess.Send(ctx, transport.TypeLogUnsubscribe,
		transport.LogUnsubscribe{SubID: "test-1"}); err != nil {
		t.Fatalf("abbestellung senden: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if unsubscribed.Load() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("die abbestellung kam beim agenten nicht an — log-streams würden dort weiterlaufen")
}

// controlplaneEnroller baut einen Enroller für Transport-nahe Tests.
func controlplaneEnroller(t *testing.T, st store.Store) *controlplane.Enroller {
	t.Helper()
	return controlplane.NewEnroller(st, quietLogger())
}

// --- Hilfsfunktionen ---

func wsAgentURL(base string) string {
	return "ws" + strings.TrimPrefix(base, "http") + "/agent"
}

// reportingAgent verhält sich wie ein echter Agent: Er meldet nach dem
// Verbinden einen Zustand. Ohne das käme in der API nie ein Container an, und
// der Test prüfte nichts.
func reportingAgent(t *testing.T, url, token, cred string, credSink *atomic.Value) *transport.Client {
	t.Helper()

	var client *transport.Client
	client = transport.NewClient(transport.ClientConfig{
		ServerURL: url,
		Hello: transport.Hello{
			AgentVersion: "test", Hostname: "nas-01",
			EnrollToken: token, Credential: cred,
			OS: "linux", Arch: "amd64",
			Capabilities: []string{"read", "lifecycle", "logs"},
		},
		Logger:       quietLogger(),
		OnCredential: func(c string) error { credSink.Store(c); return nil },
		OnConnected: func(transport.HelloAck) {
			go func() {
				env, err := transport.NewEnvelope(transport.TypeReportState, "",
					transport.StateReport{
						ObservedAt: time.Now().UTC(),
						Resources: []transport.ResourceState{{
							ID: "c1", Name: "test-web-1", Kind: "container",
							Stack: "test", Image: "nginx:1.27", State: "running",
							Labels: map[string]string{"com.docker.compose.service": "web"},
						}},
					})
				if err != nil {
					return
				}
				_ = client.Send(context.Background(), env)
			}()
		},
		BackoffMin: 20 * time.Millisecond,
		BackoffMax: 200 * time.Millisecond,
	})
	return client
}

func waitForHostID(t *testing.T, c *apiClient, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		_, out := c.do("GET", "/api/v1/hosts", nil)
		hosts, _ := out["hosts"].([]any)
		if len(hosts) > 0 {
			h, _ := hosts[0].(map[string]any)
			if id, _ := h["id"].(string); id != "" {
				return id
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("kein host innerhalb von %s", d)
	return ""
}

func waitForContainers(t *testing.T, c *apiClient, n int, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		_, out := c.do("GET", "/api/v1/containers", nil)
		containers, _ := out["containers"].([]any)
		if len(containers) >= n {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// newTestRepo legt ein echtes lokales Repo an, das als Gegenstelle taugt.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")

	run := func(wd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = wd
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost",
			"GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if err := os.MkdirAll(filepath.Join(work, "stacks", "nas-01", "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(work, "stacks", "nas-01", "test", "compose.yaml"),
		[]byte("services:\n  web:\n    image: nginx:1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(dir, "init", "-q", "-b", "main", work)
	run(work, "add", "-A")
	run(work, "commit", "-qm", "erster stack")

	bare := filepath.Join(dir, "origin.git")
	run(dir, "clone", "-q", "--bare", work, bare)
	return bare
}
