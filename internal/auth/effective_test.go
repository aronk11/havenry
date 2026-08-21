package auth_test

import (
	"testing"

	"github.com/aronk11/havenry/internal/auth"
)

// Die Auflösung ist die heikelste Stelle des Rechtemodells: Ein Fehler hier
// gibt jemandem Zugriff auf einen Host, den er nicht sehen sollte, und zwar
// lautlos. Deshalb prüft jeder Test hier eine Behauptung aus ADR-0029.

func TestResolveTakesStrongestRole(t *testing.T) {
	cases := []struct {
		name   string
		grants []auth.Grant
		want   auth.Role
	}{
		{
			"team hebt an",
			[]auth.Grant{
				{Source: "direct", Role: auth.RoleViewer},
				{Source: "team:ops", Role: auth.RoleOperator},
			},
			auth.RoleOperator,
		},
		{
			"direkte rolle bleibt, wenn sie die stärkste ist",
			[]auth.Grant{
				{Source: "direct", Role: auth.RoleAdmin},
				{Source: "team:gaeste", Role: auth.RoleViewer},
			},
			auth.RoleAdmin,
		},
		{
			"mehrere teams, stärkste gewinnt",
			[]auth.Grant{
				{Source: "direct", Role: auth.RoleViewer},
				{Source: "team:a", Role: auth.RoleViewer},
				{Source: "team:b", Role: auth.RoleAdmin},
				{Source: "team:c", Role: auth.RoleOperator},
			},
			auth.RoleAdmin,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := auth.Resolve("u1", "aron", c.grants).Role
			if got != c.want {
				t.Fatalf("Rolle = %q, erwartet %q", got, c.want)
			}
		})
	}
}

// TestTeamNeverRemovesRights hält die Kernzusage von ADR-0029 fest:
// Teams fügen hinzu, sie nehmen nichts weg.
func TestTeamNeverRemovesRights(t *testing.T) {
	// Ein Admin, der einem Gäste-Team beitritt, bleibt Admin.
	id := auth.Resolve("u1", "aron", []auth.Grant{
		{Source: "direct", Role: auth.RoleAdmin},
		{Source: "team:gaeste", Role: auth.RoleViewer, HostIDs: []string{"host-media"}},
	})
	if id.Role != auth.RoleAdmin {
		t.Fatalf("Rolle = %q — ein Team hat Rechte entzogen", id.Role)
	}
	if !id.CanAccessHost("host-fremd") {
		t.Fatal("Host-Zugriff wurde durch eine Team-Mitgliedschaft eingeschränkt")
	}
}

// TestHostSetsAreUnioned prüft die Vereinigung der Host-Mengen.
func TestHostSetsAreUnioned(t *testing.T) {
	id := auth.Resolve("u1", "mitbewohner", []auth.Grant{
		{Source: "direct", Role: auth.RoleViewer, HostIDs: []string{"host-media"}},
		{Source: "team:backup", Role: auth.RoleOperator, HostIDs: []string{"host-nas", "host-media"}},
	})

	for _, h := range []string{"host-media", "host-nas"} {
		if !id.CanAccessHost(h) {
			t.Errorf("Host %q sollte erlaubt sein", h)
		}
	}
	if id.CanAccessHost("host-privat") {
		t.Error("ein nicht zugewiesener Host wurde freigegeben")
	}
	if len(id.HostIDs) != 2 {
		t.Errorf("HostIDs = %v, erwartet zwei ohne Dopplung", id.HostIDs)
	}
}

// TestEmptyHostSetMeansAllAndWins ist der subtilste Fall.
//
// Eine leere Menge heißt "alle Hosts". Kommt sie irgendwo vor, muss sie für
// die gesamte Identität gelten — sonst würde eine Quelle, die alles erlaubt,
// durch eine engere beschnitten, und das wäre Entzug.
func TestEmptyHostSetMeansAllAndWins(t *testing.T) {
	id := auth.Resolve("u1", "aron", []auth.Grant{
		{Source: "direct", Role: auth.RoleOperator, HostIDs: []string{"host-media"}},
		{Source: "team:alle", Role: auth.RoleViewer}, // leer = alle Hosts
	})

	if len(id.HostIDs) != 0 {
		t.Fatalf("HostIDs = %v, erwartet leer (= alle Hosts)", id.HostIDs)
	}
	if !id.CanAccessHost("irgendein-host") {
		t.Fatal("die unbeschränkte Quelle wurde durch die engere beschnitten")
	}
}

// TestUnknownRoleContributesNothing: Eine kaputte oder zukünftige Rolle darf
// nicht versehentlich alles erlauben.
func TestUnknownRoleContributesNothing(t *testing.T) {
	// Die Direktzuweisung muss selbst beschränkt sein, sonst bedeutet sie
	// "alle Hosts" und die Behauptung über host-x wäre gar nicht prüfbar.
	// Die erste Fassung dieses Tests hatte genau diesen Fehler.
	id := auth.Resolve("u1", "aron", []auth.Grant{
		{Source: "direct", Role: auth.RoleViewer, HostIDs: []string{"host-erlaubt"}},
		{Source: "team:kaputt", Role: auth.Role("superuser"), HostIDs: []string{"host-x"}},
	})

	if id.Role != auth.RoleViewer {
		t.Fatalf("Rolle = %q — eine unbekannte Rolle hat gewirkt", id.Role)
	}
	if id.Can(auth.PermManageUsers) {
		t.Fatal("unbekannte Rolle hat Rechte verliehen")
	}
	// Die Host-Menge einer ungültigen Quelle darf ebenfalls nicht zählen.
	if id.CanAccessHost("host-x") {
		t.Fatal("Host aus einer ungültigen Rechtequelle wurde freigegeben")
	}
}

func TestResolveWithNoGrantsHasNoRights(t *testing.T) {
	id := auth.Resolve("u1", "niemand", nil)
	if id.Role != "" {
		t.Fatalf("Rolle = %q, erwartet leer", id.Role)
	}
	for _, p := range []auth.Permission{
		auth.PermViewHosts, auth.PermControlDocker, auth.PermManageUsers,
	} {
		if id.Can(p) {
			t.Errorf("ohne Rechtequelle wurde %q erlaubt", p)
		}
	}
}

// TestSourcesAreRecorded: Ohne die Herkunft ist im Nachhinein nicht mehr zu
// klären, warum jemand etwas durfte (ADR-0029).
func TestSourcesAreRecorded(t *testing.T) {
	id := auth.Resolve("u1", "aron", []auth.Grant{
		{Source: "direct", Role: auth.RoleViewer},
		{Source: "team:ops", Role: auth.RoleOperator},
	})

	via := id.Via()
	for _, want := range []string{"direct", "team:ops"} {
		if !contains(via, want) {
			t.Errorf("Via() = %q, sollte %q nennen", via, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle ||
			len(needle) > 0 && indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
