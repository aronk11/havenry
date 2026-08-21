// Package storetest enthält die Konformitätssuite für Store-Backends.
//
// Ein Paket, keine Testdatei — genau das macht die Datenbank austauschbar
// (ADR-0031). Ein neues Backend braucht eine einzige Testdatei:
//
//	func TestConformance(t *testing.T) {
//	    storetest.Run(t, func(t *testing.T) store.Full { return openMyBackend(t) })
//	}
//
// Ohne diese Suite wäre "austauschbar" eine Behauptung. Mit ihr ist es eine
// Prüfung, die entweder besteht oder nicht.
//
// Die Tests prüfen bewusst nicht "speichert es", sondern die Zusagen, an denen
// sich Implementierungen unterscheiden: Eindeutigkeit, Aufräumen von
// Referenzen, Atomarität, Ablauf und die Frage, was eine leere Liste bedeutet.
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aronk11/havenry/internal/store"
)

// Factory öffnet einen frischen, leeren Store für einen Test.
//
// Der Aufrufer ist für das Aufräumen zuständig (t.Cleanup). Jeder Test bekommt
// einen eigenen — sonst hängt das Ergebnis von der Reihenfolge ab.
type Factory func(t *testing.T) store.Full

// Run führt die gesamte Suite aus.
func Run(t *testing.T, open Factory) {
	t.Helper()

	tests := []struct {
		name string
		fn   func(t *testing.T, s store.Full)
	}{
		{"EnrollTokenLifecycle", testEnrollTokenLifecycle},
		{"EnrollTokenConsumedOnce", testEnrollTokenConsumedOnce},
		{"HostApprovalSurvivesUpsert", testHostApprovalSurvivesUpsert},
		{"HostLookupAndNotFound", testHostLookupAndNotFound},
		{"EventsRoundTrip", testEventsRoundTrip},

		{"UsernameUniqueCaseInsensitive", testUsernameUniqueCaseInsensitive},
		{"DeletingUserCleansUpReferences", testDeletingUserCleansUpReferences},
		{"ExpiredSessionIsNotFound", testExpiredSessionIsNotFound},
		{"ExpiredAPITokenIsRejected", testExpiredAPITokenIsRejected},
		{"PurgeKeepsValidSessions", testPurgeKeepsValidSessions},

		{"TeamNameUnique", testTeamNameUnique},
		{"TeamMembershipRoundTrip", testTeamMembershipRoundTrip},
		{"AddTeamMemberIsIdempotent", testAddTeamMemberIsIdempotent},
		{"AddTeamMemberRejectsUnknownIDs", testAddTeamMemberRejectsUnknownIDs},
		{"DeletingTeamRemovesMemberships", testDeletingTeamRemovesMemberships},
		{"UpdateTeamKeepsMembers", testUpdateTeamKeepsMembers},
		{"EmptyHostListStaysEmpty", testEmptyHostListStaysEmpty},

		{"RepoIsSingleton", testRepoIsSingleton},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, open(t))
		})
	}
}

// --- Enrollment ----------------------------------------------------------

func testEnrollTokenLifecycle(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateEnrollToken(ctx, store.EnrollToken{
		TokenHash: "gueltig", CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}))

	must(t, s.ConsumeEnrollToken(ctx, "gueltig", now))

	// Ein Token wirkt genau einmal (ADR-0015).
	if err := s.ConsumeEnrollToken(ctx, "gueltig", now); !errors.Is(err, store.ErrTokenUsed) {
		t.Fatalf("zweite Einlösung = %v, erwartet ErrTokenUsed", err)
	}
	if err := s.ConsumeEnrollToken(ctx, "gibtsnicht", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unbekanntes Token = %v, erwartet ErrNotFound", err)
	}

	must(t, s.CreateEnrollToken(ctx, store.EnrollToken{
		TokenHash: "abgelaufen", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute),
	}))
	if err := s.ConsumeEnrollToken(ctx, "abgelaufen", now); !errors.Is(err, store.ErrTokenExpired) {
		t.Fatalf("abgelaufenes Token = %v, erwartet ErrTokenExpired", err)
	}
}

// testEnrollTokenConsumedOnce prüft die Atomarität.
//
// Zwei Agenten lösen dasselbe Token gleichzeitig ein. Gewinnen beide, hängt ein
// zweiter, fremder Host am System. Ein Backend, das Prüfen und Entwerten in
// zwei Schritten macht, fällt hier durch.
func testEnrollTokenConsumedOnce(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateEnrollToken(ctx, store.EnrollToken{
		TokenHash: "wettlauf", CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}))

	const parallel = 16
	var wg sync.WaitGroup
	results := make([]error, parallel)
	start := make(chan struct{})

	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = s.ConsumeEnrollToken(ctx, "wettlauf", now)
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d von %d gleichzeitigen Einlösungen erfolgreich, erwartet genau 1", wins, parallel)
	}
}

// --- Hosts ---------------------------------------------------------------

// testHostApprovalSurvivesUpsert: Eine Neuverbindung darf einen bestätigten
// Host nicht zurücksetzen — sonst müsste der Nutzer nach jedem Agent-Neustart
// erneut bestätigen.
func testHostApprovalSurvivesUpsert(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	h := store.Host{
		ID: "host-1", Hostname: "nas-01", CredentialHash: "cred-1",
		OS: "linux", Arch: "arm64", AgentVersion: "1.0.0",
		EnrolledAt: now, LastSeen: now,
	}
	must(t, s.UpsertHost(ctx, h))
	must(t, s.ApproveHost(ctx, "host-1"))

	h.AgentVersion = "1.1.0"
	h.Hostname = "nas-01-umbenannt"
	h.Approved = false // der Agent weiß nichts von der Bestätigung
	h.LastSeen = now.Add(time.Minute)
	must(t, s.UpsertHost(ctx, h))

	got, err := s.HostByID(ctx, "host-1")
	must(t, err)
	if !got.Approved {
		t.Fatal("die Bestätigung ging beim Upsert verloren")
	}
	if got.AgentVersion != "1.1.0" || got.Hostname != "nas-01-umbenannt" {
		t.Fatalf("aktualisierte Felder nicht übernommen: %+v", got)
	}
	if !got.EnrolledAt.Equal(now) {
		t.Fatalf("EnrolledAt = %v, erwartet %v — der Upsert hat es überschrieben", got.EnrolledAt, now)
	}
}

func testHostLookupAndNotFound(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := s.HostByID(ctx, "weg"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("HostByID = %v, erwartet ErrNotFound", err)
	}
	if _, err := s.HostByCredentialHash(ctx, "weg"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("HostByCredentialHash = %v, erwartet ErrNotFound", err)
	}
	if err := s.ApproveHost(ctx, "weg"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ApproveHost = %v, erwartet ErrNotFound", err)
	}

	for _, name := range []string{"beta", "alpha"} {
		must(t, s.UpsertHost(ctx, store.Host{
			ID: name, Hostname: name, CredentialHash: "cred-" + name,
			EnrolledAt: now, LastSeen: now,
		}))
	}

	got, err := s.HostByCredentialHash(ctx, "cred-alpha")
	if err != nil || got.ID != "alpha" {
		t.Fatalf("Suche über Credential-Hash: %v / %+v", err, got)
	}
	hosts, err := s.Hosts(ctx)
	if err != nil || len(hosts) != 2 {
		t.Fatalf("Hosts() = %d Einträge, %v", len(hosts), err)
	}
}

// --- Ereignisse ----------------------------------------------------------

func testEventsRoundTrip(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	for i := 0; i < 5; i++ {
		must(t, s.AppendEvent(ctx, store.Event{
			At: now.Add(time.Duration(i) * time.Second), HostID: "host-1",
			Kind: "test.event", Actor: "aron", Summary: "Ereignis",
			Details: map[string]string{"index": string(rune('a' + i))},
		}))
	}

	events, err := s.Events(ctx, 3)
	must(t, err)
	if len(events) != 3 {
		t.Fatalf("%d Ereignisse, erwartet 3", len(events))
	}
	// Die neuesten drei, aufsteigend sortiert.
	if !events[0].At.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("erstes Ereignis at = %v, erwartet %v", events[0].At, now.Add(2*time.Second))
	}
	if events[2].Details["index"] != "e" {
		t.Fatalf("Details nicht korrekt übertragen: %+v", events[2].Details)
	}
}

// --- Nutzer, Sitzungen, Token --------------------------------------------

func user(name string) store.User {
	return store.User{
		ID: "u-" + name, Username: name, PasswordHash: "hash-" + name,
		Role: "viewer", CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func testUsernameUniqueCaseInsensitive(t *testing.T, s store.Full) {
	ctx := context.Background()
	must(t, s.CreateUser(ctx, user("aron")))

	// Sonst existieren "aron" und "Aron" nebeneinander, und niemand weiß,
	// wer sich gerade angemeldet hat.
	other := user("aron")
	other.ID = "u-zweiter"
	other.Username = "ARON"
	if err := s.CreateUser(ctx, other); !errors.Is(err, store.ErrUserExists) {
		t.Fatalf("andere Schreibweise = %v, erwartet ErrUserExists", err)
	}
	if _, err := s.UserByName(ctx, "ArOn"); err != nil {
		t.Fatalf("UserByName ist schreibungsabhängig: %v", err)
	}
}

// testDeletingUserCleansUpReferences: Ein gültiges Token eines gelöschten
// Nutzers wäre ein Zugang, den niemand mehr sieht.
func testDeletingUserCleansUpReferences(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateUser(ctx, user("aron")))
	must(t, s.CreateSession(ctx, store.Session{
		TokenHash: "sess-1", UserID: "u-aron", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	must(t, s.CreateAPIToken(ctx, store.APIToken{
		ID: "t-1", TokenHash: "tok-1", UserID: "u-aron", Name: "bot", CreatedAt: now,
	}))
	must(t, s.CreateTeam(ctx, store.Team{
		ID: "team-1", Name: "ops", Role: "operator", CreatedAt: now,
	}))
	must(t, s.AddTeamMember(ctx, "team-1", "u-aron", now))

	must(t, s.DeleteUser(ctx, "u-aron"))

	if _, err := s.SessionByHash(ctx, "sess-1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Sitzung überlebte das Löschen des Nutzers: %v", err)
	}
	if _, err := s.APITokenByHash(ctx, "tok-1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("API-Token überlebte das Löschen des Nutzers: %v", err)
	}
	members, err := s.TeamMembers(ctx, "team-1")
	must(t, err)
	if len(members) != 0 {
		t.Errorf("Mitgliedschaft überlebte das Löschen des Nutzers: %v", members)
	}

	// TeamMembers allein genügt als Prüfung nicht: Eine Implementierung, die
	// beim Auflisten unbekannte Nutzer stillschweigend überspringt, sieht
	// sauber aus und hat trotzdem eine verwaiste Mitgliedschaft liegen.
	// Sichtbar wird sie, wenn ein Nutzer mit derselben ID neu entsteht — dann
	// erbt er plötzlich fremde Rechte.
	must(t, s.CreateUser(ctx, user("aron")))
	teams, err := s.TeamsForUser(ctx, "u-aron")
	must(t, err)
	if len(teams) != 0 {
		t.Errorf("ein neuer Nutzer mit derselben ID erbte %d Team(s) — "+
			"die alte Mitgliedschaft wurde nicht aufgeräumt", len(teams))
	}
}

// testExpiredSessionIsNotFound: Die Ablaufprüfung gehört in die Abfrage, nicht
// in den Aufrufer — sonst vergisst sie irgendwann jemand.
func testExpiredSessionIsNotFound(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateUser(ctx, user("aron")))
	must(t, s.CreateSession(ctx, store.Session{
		TokenHash: "alt", UserID: "u-aron",
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Minute),
	}))

	if _, err := s.SessionByHash(ctx, "alt"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("abgelaufene Sitzung wurde geliefert: %v", err)
	}
}

func testExpiredAPITokenIsRejected(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Minute)

	must(t, s.CreateUser(ctx, user("bot")))
	must(t, s.CreateAPIToken(ctx, store.APIToken{
		ID: "t-1", TokenHash: "alt", UserID: "u-bot", Name: "alt",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: &past,
	}))

	if _, err := s.APITokenByHash(ctx, "alt"); !errors.Is(err, store.ErrTokenExpired) {
		t.Fatalf("abgelaufenes Token = %v, erwartet ErrTokenExpired", err)
	}
}

func testPurgeKeepsValidSessions(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateUser(ctx, user("aron")))
	for _, sess := range []store.Session{
		{TokenHash: "alt", UserID: "u-aron", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)},
		{TokenHash: "gueltig", UserID: "u-aron", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
	} {
		must(t, s.CreateSession(ctx, sess))
	}

	must(t, s.PurgeExpiredSessions(ctx, now))
	if _, err := s.SessionByHash(ctx, "gueltig"); err != nil {
		t.Fatalf("gültige Sitzung wurde mit aufgeräumt: %v", err)
	}
}

// --- Teams (ADR-0029) ----------------------------------------------------

func testTeamNameUnique(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	base := store.Team{ID: "t-1", Name: "ops", Role: "operator", CreatedAt: now}
	must(t, s.CreateTeam(ctx, base))

	dup := base
	dup.ID = "t-2"
	dup.Name = "OPS"
	if err := s.CreateTeam(ctx, dup); !errors.Is(err, store.ErrTeamExists) {
		t.Fatalf("doppelter Teamname = %v, erwartet ErrTeamExists", err)
	}
}

func testTeamMembershipRoundTrip(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateTeam(ctx, store.Team{
		ID: "t-ops", Name: "ops", Role: "operator",
		HostIDs: []string{"h-1", "h-2"}, CreatedAt: now,
	}))
	for _, n := range []string{"aron", "mitbewohner"} {
		must(t, s.CreateUser(ctx, user(n)))
		must(t, s.AddTeamMember(ctx, "t-ops", "u-"+n, now))
	}

	members, err := s.TeamMembers(ctx, "t-ops")
	if err != nil || len(members) != 2 {
		t.Fatalf("TeamMembers = %d, %v", len(members), err)
	}

	teams, err := s.TeamsForUser(ctx, "u-aron")
	if err != nil || len(teams) != 1 {
		t.Fatalf("TeamsForUser = %d, %v", len(teams), err)
	}
	// Die Host-Menge entscheidet über den Zugriff — sie muss den Weg durch
	// die Kodierung unverändert überstehen.
	if len(teams[0].HostIDs) != 2 || teams[0].HostIDs[0] != "h-1" {
		t.Fatalf("HostIDs = %v", teams[0].HostIDs)
	}
}

func testAddTeamMemberIsIdempotent(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateTeam(ctx, store.Team{ID: "t-1", Name: "ops", Role: "viewer", CreatedAt: now}))
	must(t, s.CreateUser(ctx, user("aron")))

	for i := 0; i < 3; i++ {
		if err := s.AddTeamMember(ctx, "t-1", "u-aron", now); err != nil {
			t.Fatalf("Versuch %d: %v", i+1, err)
		}
	}
	members, _ := s.TeamMembers(ctx, "t-1")
	if len(members) != 1 {
		t.Fatalf("%d Mitglieder nach dreifachem Hinzufügen", len(members))
	}
}

func testAddTeamMemberRejectsUnknownIDs(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateTeam(ctx, store.Team{ID: "t-1", Name: "ops", Role: "viewer", CreatedAt: now}))
	must(t, s.CreateUser(ctx, user("aron")))

	if err := s.AddTeamMember(ctx, "t-weg", "u-aron", now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unbekanntes Team = %v, erwartet ErrNotFound", err)
	}
	if err := s.AddTeamMember(ctx, "t-1", "u-weg", now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unbekannter Nutzer = %v, erwartet ErrNotFound", err)
	}
}

// testDeletingTeamRemovesMemberships: Bliebe eine Mitgliedschaft zu einem
// gelöschten Team zurück, stolperte die Rechteauflösung beim nächsten Lesen
// darüber.
func testDeletingTeamRemovesMemberships(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateTeam(ctx, store.Team{ID: "t-1", Name: "ops", Role: "operator", CreatedAt: now}))
	must(t, s.CreateUser(ctx, user("aron")))
	must(t, s.AddTeamMember(ctx, "t-1", "u-aron", now))

	must(t, s.DeleteTeam(ctx, "t-1"))

	teams, err := s.TeamsForUser(ctx, "u-aron")
	must(t, err)
	if len(teams) != 0 {
		t.Fatalf("Mitgliedschaft überlebte das Löschen des Teams: %v", teams)
	}
}

func testUpdateTeamKeepsMembers(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	tm := store.Team{ID: "t-1", Name: "ops", Role: "viewer", CreatedAt: now}
	must(t, s.CreateTeam(ctx, tm))
	must(t, s.CreateUser(ctx, user("aron")))
	must(t, s.AddTeamMember(ctx, "t-1", "u-aron", now))

	tm.Role = "operator"
	tm.HostIDs = []string{"h-1"}
	must(t, s.UpdateTeam(ctx, tm))

	members, _ := s.TeamMembers(ctx, "t-1")
	if len(members) != 1 {
		t.Fatal("Mitglieder gingen bei der Änderung verloren")
	}
	got, _ := s.TeamByID(ctx, "t-1")
	if got.Role != "operator" || len(got.HostIDs) != 1 {
		t.Fatalf("Änderung nicht übernommen: %+v", got)
	}
}

// testEmptyHostListStaysEmpty: Käme eine leere Liste als Liste mit einem
// leeren Eintrag zurück, hieße "alle Hosts" plötzlich "genau ein Host ohne
// Namen" — also gar keiner.
func testEmptyHostListStaysEmpty(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC()

	must(t, s.CreateTeam(ctx, store.Team{
		ID: "t-1", Name: "alle", Role: "viewer", HostIDs: nil, CreatedAt: now,
	}))
	got, err := s.TeamByID(ctx, "t-1")
	must(t, err)
	if len(got.HostIDs) != 0 {
		t.Fatalf("HostIDs = %v (%d Einträge), erwartet leer", got.HostIDs, len(got.HostIDs))
	}
}

// --- Repo ----------------------------------------------------------------

func testRepoIsSingleton(t *testing.T, s store.Full) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, err := s.Repo(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Repo ohne Konfiguration = %v, erwartet ErrNotFound", err)
	}

	for _, url := range []string{"https://a.invalid/x.git", "https://b.invalid/y.git"} {
		must(t, s.SaveRepo(ctx, store.GitRepo{URL: url, Branch: "main", ConfiguredAt: now}))
	}

	got, err := s.Repo(ctx)
	must(t, err)
	if got.URL != "https://b.invalid/y.git" {
		t.Fatalf("URL = %q, erwartet die zuletzt gespeicherte — es gibt genau ein Repo", got.URL)
	}

	must(t, s.ClearRepo(ctx))
	if _, err := s.Repo(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("nach ClearRepo = %v, erwartet ErrNotFound", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
