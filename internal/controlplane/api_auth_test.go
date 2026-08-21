package controlplane_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aronk11/havenry/internal/auth"
	"github.com/aronk11/havenry/internal/controlplane"
	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/store/sqlitestore"
)

// Diese Tests prüfen die Rechte auf HTTP-Ebene, nicht in der Bibliothek.
//
// Das ist der Unterschied, auf den es ankommt: Die Rechteprüfung in
// internal/auth kann korrekt sein und die API trotzdem offen, wenn eine Route
// die Middleware nicht durchläuft. Getestet wird deshalb genau das, was ein
// Angreifer auch täte — ein HTTP-Aufruf.

type apiClient struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
	bearer string
}

func newTestServer(t *testing.T) (*controlplane.Server, *httptest.Server, store.Full) {
	t.Helper()

	dir := t.TempDir()
	st, err := sqlitestore.OpenSQLite(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := controlplane.NewServer(st, "test", filepath.Join(dir, "repo"), quietLogger())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts, st
}

func (c *apiClient) do(method, path string, body any) (*http.Response, map[string]any) {
	c.t.Helper()

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()

	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

// login meldet sich an und übernimmt das Sitzungs-Cookie.
func (c *apiClient) login(username, password string) {
	c.t.Helper()
	resp, out := c.do("POST", "/api/v1/auth/login",
		map[string]string{"username": username, "password": password})
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("anmeldung als %q fehlgeschlagen: %d %v", username, resp.StatusCode, out)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "havenry_session" {
			c.cookie = ck
			return
		}
	}
	c.t.Fatal("kein sitzungs-cookie gesetzt")
}

// adminPassword setzt ein bekanntes Passwort für den erzeugten Admin.
// Das Startpasswort steht nur im Protokoll — im Test wird es direkt ersetzt.
func adminPassword(t *testing.T, st store.Full, password string) {
	t.Helper()
	u, err := st.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatalf("admin nicht gefunden — wurde er beim start angelegt? %v", err)
	}
	hash, err := hashFor(password)
	if err != nil {
		t.Fatal(err)
	}
	u.PasswordHash = hash
	u.MustChangePassword = false
	if err := st.UpdateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

// TestInitialAdminIsCreated belegt ADR-0022: kein Standardpasswort, aber auch
// kein Zustand ohne jeden Zugang.
func TestInitialAdminIsCreated(t *testing.T) {
	_, _, st := newTestServer(t)

	n, err := st.CountUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d nutzer nach erstem start, erwartet 1", n)
	}
	u, err := st.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != "admin" {
		t.Errorf("rolle = %q", u.Role)
	}
	if !u.MustChangePassword {
		t.Error("startpasswort ohne änderungszwang")
	}
	if u.PasswordHash == "" {
		t.Error("kein passwort-hash gesetzt")
	}
}

// TestUnauthenticatedIsRejected ist der Test, den es in M2 noch nicht gab:
// Ohne Anmeldung geht nichts.
func TestUnauthenticatedIsRejected(t *testing.T) {
	_, ts, _ := newTestServer(t)
	c := &apiClient{t: t, base: ts.URL}

	geschuetzt := []struct{ method, path string }{
		{"GET", "/api/v1/hosts"},
		{"GET", "/api/v1/stacks"},
		{"GET", "/api/v1/containers"},
		{"GET", "/api/v1/events"},
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/enroll-tokens"},
		{"GET", "/api/v1/repo"},
		{"POST", "/api/v1/containers/host-1/abc/restart"},
		{"GET", "/api/v1/containers/host-1/abc/logs"},
	}
	for _, r := range geschuetzt {
		resp, _ := c.do(r.method, r.path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s ohne anmeldung = %d, erwartet 401",
				r.method, r.path, resp.StatusCode)
		}
	}

	// Der Gesundheitsendpunkt bleibt offen — er verrät nichts.
	resp, _ := c.do("GET", "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d, sollte offen bleiben", resp.StatusCode)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "korrektes-langes-passwort")
	c := &apiClient{t: t, base: ts.URL}

	resp, _ := c.do("POST", "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "falsch-aber-lang-genug"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("falsches passwort = %d, erwartet 401", resp.StatusCode)
	}

	// Unbekannter Nutzer muss dieselbe Antwort liefern — sonst lässt sich
	// herausfinden, welche Benutzernamen existieren.
	resp2, out2 := c.do("POST", "/api/v1/auth/login",
		map[string]string{"username": "gibtesnicht", "password": "falsch-aber-lang-genug"})
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unbekannter nutzer = %d, erwartet 401", resp2.StatusCode)
	}
	_, out1 := c.do("POST", "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "falsch-aber-lang-genug"})
	if out1["error"] != out2["error"] {
		t.Errorf("unterschiedliche fehlermeldungen verraten die existenz eines nutzers:\n  %v\n  %v",
			out1["error"], out2["error"])
	}
}

// TestViewerCannotControlContainers prüft die Rollentrennung über HTTP.
func TestViewerCannotControlContainers(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")

	resp, _ := admin.do("POST", "/api/v1/users", map[string]any{
		"username": "gast", "password": "gast-passwort-lang-genug",
		"role": "viewer", "host_ids": []string{},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("nutzer anlegen = %d", resp.StatusCode)
	}

	gast := &apiClient{t: t, base: ts.URL}
	gast.login("gast", "gast-passwort-lang-genug")

	// Lesen darf er.
	if resp, _ := gast.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("viewer darf hosts nicht lesen: %d", resp.StatusCode)
	}
	// Steuern nicht.
	if resp, _ := gast.do("POST", "/api/v1/containers/host-1/abc/restart", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer konnte container steuern: %d, erwartet 403", resp.StatusCode)
	}
	// Nutzer verwalten nicht.
	if resp, _ := gast.do("GET", "/api/v1/users", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer sieht die nutzerliste: %d, erwartet 403", resp.StatusCode)
	}
	if resp, _ := gast.do("POST", "/api/v1/users", map[string]any{
		"username": "eigener-admin", "password": "sehr-langes-passwort", "role": "admin",
	}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer konnte einen admin anlegen: %d — schwere rechteausweitung", resp.StatusCode)
	}
	// Repo nicht ändern.
	if resp, _ := gast.do("PUT", "/api/v1/repo", map[string]any{
		"url": "https://example.invalid/repo.git", "branch": "main",
	}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer konnte das repo ändern: %d", resp.StatusCode)
	}
}

// TestOperatorRestrictedToItsHosts ist der Kern von ADR-0022:
// Rolle allein genügt nicht, der Host muss erlaubt sein.
func TestOperatorRestrictedToItsHosts(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")
	ctx := context.Background()

	// Zwei Hosts anlegen.
	for _, h := range []store.Host{
		{ID: "host-media", Hostname: "nas-media", CredentialHash: "c1"},
		{ID: "host-privat", Hostname: "nas-privat", CredentialHash: "c2"},
	} {
		if err := st.UpsertHost(ctx, h); err != nil {
			t.Fatal(err)
		}
	}

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")

	resp, _ := admin.do("POST", "/api/v1/users", map[string]any{
		"username": "mitbewohner", "password": "mitbewohner-passwort-lang",
		"role": "operator", "host_ids": []string{"host-media"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("nutzer anlegen = %d", resp.StatusCode)
	}

	mb := &apiClient{t: t, base: ts.URL}
	mb.login("mitbewohner", "mitbewohner-passwort-lang")

	// Er sieht nur seinen Host.
	_, out := mb.do("GET", "/api/v1/hosts", nil)
	hosts, _ := out["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("beschränkter nutzer sieht %d hosts, erwartet 1: %v", len(hosts), out)
	}
	if h, _ := hosts[0].(map[string]any); h["id"] != "host-media" {
		t.Fatalf("falscher host sichtbar: %v", hosts[0])
	}

	// Auf dem fremden Host: 404, nicht 403 — die Existenz wird nicht verraten.
	resp, _ = mb.do("POST", "/api/v1/containers/host-privat/abc/restart", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("aktion auf fremdem host = %d, erwartet 404", resp.StatusCode)
	}
	resp, _ = mb.do("GET", "/api/v1/containers/host-privat/abc/logs", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("logs auf fremdem host = %d, erwartet 404", resp.StatusCode)
	}

	// Auf dem eigenen Host darf er — der Host ist nur gerade nicht verbunden,
	// also 503 statt 403/404. Wichtig ist: die Rechteprüfung lässt ihn durch.
	resp, _ = mb.do("POST", "/api/v1/containers/host-media/abc/restart", nil)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		t.Errorf("aktion auf eigenem host abgelehnt: %d", resp.StatusCode)
	}
}

// TestAPITokenInheritsUserRights belegt, dass ein Token nie mehr darf als
// der Nutzer dahinter.
func TestAPITokenInheritsUserRights(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")
	admin.do("POST", "/api/v1/users", map[string]any{
		"username": "bot", "password": "bot-passwort-lang-genug", "role": "viewer",
	})

	bot := &apiClient{t: t, base: ts.URL}
	bot.login("bot", "bot-passwort-lang-genug")

	resp, out := bot.do("POST", "/api/v1/auth/tokens", map[string]any{"name": "nächtliche prüfung"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("token erstellen = %d: %v", resp.StatusCode, out)
	}
	secret, _ := out["token"].(string)
	if secret == "" {
		t.Fatal("kein token-geheimnis geliefert")
	}

	viaToken := &apiClient{t: t, base: ts.URL, bearer: secret}
	if resp, _ := viaToken.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("token kann nicht lesen: %d", resp.StatusCode)
	}
	// Der Nutzer ist viewer — das Token darf nicht mehr.
	if resp, _ := viaToken.do("POST", "/api/v1/containers/h/abc/restart", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("token eines viewers durfte steuern: %d", resp.StatusCode)
	}

	// Ein erfundenes Token wird abgewiesen.
	falsch := &apiClient{t: t, base: ts.URL, bearer: "ausgedachtes-token"}
	if resp, _ := falsch.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("erfundenes token = %d, erwartet 401", resp.StatusCode)
	}
}

// TestLastAdminIsProtected verhindert den Zustand, in dem niemand mehr
// verwalten kann.
func TestLastAdminIsProtected(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")

	u, err := st.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}

	resp, out := admin.do("PATCH", "/api/v1/users/"+u.ID, map[string]any{"role": "viewer"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("letzter admin konnte sich herabstufen: %d %v", resp.StatusCode, out)
	}
	resp, _ = admin.do("DELETE", "/api/v1/users/"+u.ID, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("letzter admin konnte sich löschen: %d", resp.StatusCode)
	}

	// Mit einem zweiten Admin ist die Herabstufung erlaubt.
	admin.do("POST", "/api/v1/users", map[string]any{
		"username": "zweiter", "password": "zweiter-passwort-lang", "role": "admin",
	})
	resp, out = admin.do("PATCH", "/api/v1/users/"+u.ID, map[string]any{"role": "viewer"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("herabstufung mit zweitem admin = %d: %v", resp.StatusCode, out)
	}
}

// TestRoleChangeEndsSessions: Eine alte Sitzung darf keine alten Rechte
// weitertragen.
func TestRoleChangeEndsSessions(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")
	admin.do("POST", "/api/v1/users", map[string]any{
		"username": "wechsler", "password": "wechsler-passwort-lang", "role": "operator",
	})

	w := &apiClient{t: t, base: ts.URL}
	w.login("wechsler", "wechsler-passwort-lang")
	if resp, _ := w.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("angemeldet, aber kein zugriff: %d", resp.StatusCode)
	}

	u, err := st.UserByName(context.Background(), "wechsler")
	if err != nil {
		t.Fatal(err)
	}
	admin.do("PATCH", "/api/v1/users/"+u.ID, map[string]any{"role": "viewer"})

	// Die alte Sitzung muss ungültig sein.
	if resp, _ := w.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("alte sitzung nach rollenwechsel noch gültig: %d", resp.StatusCode)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	c := &apiClient{t: t, base: ts.URL}
	c.login("admin", "admin-passwort-lang-genug")
	if resp, _ := c.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusOK {
		t.Fatal("anmeldung wirkungslos")
	}
	c.do("POST", "/api/v1/auth/logout", nil)
	if resp, _ := c.do("GET", "/api/v1/hosts", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Error("sitzung nach abmeldung noch gültig")
	}
}

// TestFailedLoginIsLogged belegt, dass das Ereignisprotokoll den Nachweis
// erbringt, für den es da ist (ADR-0018).
func TestFailedLoginIsLogged(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	c := &apiClient{t: t, base: ts.URL}
	c.do("POST", "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "falsches-langes-passwort"})
	c.login("admin", "admin-passwort-lang-genug")

	events, err := st.Events(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	var sahFehlversuch, sahAnmeldung bool
	for _, e := range events {
		switch e.Kind {
		case "auth.login_failed":
			sahFehlversuch = true
		case "auth.login":
			sahAnmeldung = true
			if e.Actor != "admin" {
				t.Errorf("anmeldung mit actor %q protokolliert", e.Actor)
			}
		}
	}
	if !sahFehlversuch {
		t.Error("fehlversuch wurde nicht protokolliert")
	}
	if !sahAnmeldung {
		t.Error("erfolgreiche anmeldung wurde nicht protokolliert")
	}
}

// TestActionIsLoggedWithUsername: Erst der Nutzername macht das Protokoll
// zum Nachweis (ADR-0022).
func TestActionIsLoggedWithUsername(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")
	ctx := context.Background()

	if err := st.UpsertHost(ctx, store.Host{
		ID: "host-1", Hostname: "nas", CredentialHash: "c1",
	}); err != nil {
		t.Fatal(err)
	}

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")
	admin.do("POST", "/api/v1/hosts/host-1/approve", nil)

	events, err := st.Events(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == "hosts.approved" {
			if e.Actor != "admin" {
				t.Fatalf("bestätigung mit actor %q protokolliert, erwartet den nutzernamen", e.Actor)
			}
			return
		}
	}
	t.Fatal("bestätigung wurde nicht protokolliert")
}

// hashFor kapselt das Passwort-Hashing für die Testeinrichtung.
func hashFor(password string) (string, error) {
	return auth.HashPassword(password)
}

// newTestStore öffnet einen frischen Store für einen Test.
//
// Die Wahl des Backends steht nur hier — der übrige Testcode kennt nur
// store.Full, genau wie der Produktivcode (ADR-0031).
func newTestStore(t *testing.T) store.Full {
	t.Helper()

	s, err := sqlitestore.OpenSQLite(context.Background(),
		filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("teststore öffnen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
