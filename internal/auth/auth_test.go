package auth_test

import (
	"strings"
	"testing"

	"github.com/aronk11/havenry/internal/auth"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	const pw = "ein-ausreichend-langes-passwort"

	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("hashen: %v", err)
	}
	// Das Passwort darf im Hash nicht auftauchen.
	if strings.Contains(hash, pw) {
		t.Fatal("passwort steht im klartext im hash")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unerwartetes hash-format: %q", hash)
	}
	if err := auth.VerifyPassword(pw, hash); err != nil {
		t.Fatalf("richtiges passwort abgelehnt: %v", err)
	}
	if err := auth.VerifyPassword(pw+"x", hash); err == nil {
		t.Fatal("falsches passwort akzeptiert")
	}
}

// TestHashesAreSalted stellt sicher, dass zwei gleiche Passwörter
// unterschiedliche Hashes ergeben. Ohne Salt ließen sich gleiche Passwörter
// zwischen Nutzern erkennen.
func TestHashesAreSalted(t *testing.T) {
	const pw = "gleiches-passwort-zweimal"
	a, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("zwei hashes desselben passworts sind identisch — kein salt?")
	}
	// Beide müssen trotzdem prüfbar sein.
	if err := auth.VerifyPassword(pw, a); err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifyPassword(pw, b); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	if _, err := auth.HashPassword("kurz"); err == nil {
		t.Fatal("zu kurzes passwort wurde akzeptiert")
	}
	if _, err := auth.HashPassword(strings.Repeat("a", 2000)); err == nil {
		t.Fatal("übermäßig langes passwort wurde akzeptiert")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{
		"", "kein-hash", "$argon2id$kaputt",
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",
	} {
		if err := auth.VerifyPassword("egal", bad); err == nil {
			t.Errorf("kaputter hash %q wurde akzeptiert", bad)
		}
	}
}

// TestRolePermissions hält die Tabelle aus ADR-0022 fest. Ändert jemand die
// Rechte einer Rolle, schlägt dieser Test fehl — genau so ist es gedacht.
func TestRolePermissions(t *testing.T) {
	cases := []struct {
		role  auth.Role
		perm  auth.Permission
		allow bool
	}{
		{auth.RoleAdmin, auth.PermManageUsers, true},
		{auth.RoleAdmin, auth.PermApproveHost, true},
		{auth.RoleAdmin, auth.PermManageRepo, true},
		{auth.RoleAdmin, auth.PermControlDocker, true},

		{auth.RoleOperator, auth.PermControlDocker, true},
		{auth.RoleOperator, auth.PermViewLogs, true},
		{auth.RoleOperator, auth.PermManageUsers, false},
		{auth.RoleOperator, auth.PermApproveHost, false},
		{auth.RoleOperator, auth.PermManageRepo, false},

		{auth.RoleViewer, auth.PermViewHosts, true},
		{auth.RoleViewer, auth.PermViewLogs, true},
		{auth.RoleViewer, auth.PermControlDocker, false},
		{auth.RoleViewer, auth.PermAdoptRevert, false},
		{auth.RoleViewer, auth.PermManageUsers, false},
	}

	for _, c := range cases {
		id := auth.Identity{Role: c.role}
		if got := id.Can(c.perm); got != c.allow {
			t.Errorf("%s darf %s = %v, erwartet %v", c.role, c.perm, got, c.allow)
		}
	}

	// Eine unbekannte Rolle darf nichts — nicht alles.
	unknown := auth.Identity{Role: auth.Role("superuser")}
	if unknown.Can(auth.PermViewHosts) {
		t.Error("unbekannte rolle hat rechte bekommen")
	}
}

// TestHostRestriction ist der Kern des Zugriffsmodells: Rolle allein genügt
// nicht, der Host muss auch erlaubt sein (ADR-0022, ADR-0023).
func TestHostRestriction(t *testing.T) {
	beschraenkt := auth.Identity{
		Role: auth.RoleOperator, HostIDs: []string{"host-media", "host-backup"},
	}
	if !beschraenkt.CanAccessHost("host-media") {
		t.Error("erlaubter host wurde abgelehnt")
	}
	if beschraenkt.CanAccessHost("host-produktiv") {
		t.Error("fremder host wurde freigegeben")
	}

	// Leere Liste bedeutet: alle Hosts.
	unbeschraenkt := auth.Identity{Role: auth.RoleOperator}
	if !unbeschraenkt.CanAccessHost("beliebiger-host") {
		t.Error("leere host-liste sollte alle hosts erlauben")
	}

	// Admin ignoriert die Beschränkung — sonst könnte man sich selbst
	// aussperren und niemand käme mehr an die Nutzerverwaltung.
	admin := auth.Identity{Role: auth.RoleAdmin, HostIDs: []string{"nur-dieser"}}
	if !admin.CanAccessHost("ganz-anderer") {
		t.Error("admin wurde durch host-beschränkung eingeschränkt")
	}
}

// TestTokenIdentityKeepsUserRights belegt: Ein API-Token erbt die Rechte
// seines Nutzers und kann nie mehr.
func TestTokenIdentityKeepsUserRights(t *testing.T) {
	id := auth.Identity{
		Username: "backup-bot", Role: auth.RoleViewer,
		HostIDs: []string{"host-nas"}, ViaToken: "nächtliches backup",
	}
	if id.Can(auth.PermControlDocker) {
		t.Error("token eines viewers darf container steuern")
	}
	if id.CanAccessHost("host-anderer") {
		t.Error("token umgeht die host-beschränkung")
	}
	// Das Protokoll muss Automatisierung von Handarbeit unterscheiden.
	if !strings.Contains(id.Actor(), "token") {
		t.Errorf("Actor() = %q, sollte den token-ursprung nennen", id.Actor())
	}
}

func TestUsernameValidation(t *testing.T) {
	valid := []string{"aron", "backup-bot", "user_1", "a.b", "ab"}
	for _, u := range valid {
		if err := auth.ValidateUsername(u); err != nil {
			t.Errorf("%q wurde abgelehnt: %v", u, err)
		}
	}
	invalid := []string{"", "a", strings.Repeat("x", 33), "hat leerzeichen",
		"drop;table", "../etc", "ö-umlaut"}
	for _, u := range invalid {
		if err := auth.ValidateUsername(u); err == nil {
			t.Errorf("%q wurde akzeptiert", u)
		}
	}
}

func TestSecretsAreDistinctAndHashed(t *testing.T) {
	a, err := auth.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("zwei geheimnisse sind identisch")
	}
	if len(a) < 40 {
		t.Fatalf("geheimnis zu kurz: %d zeichen", len(a))
	}
	h := auth.HashSecret(a)
	if strings.Contains(h, a) {
		t.Fatal("geheimnis steht im hash")
	}
	if h != auth.HashSecret(a) {
		t.Fatal("hash ist nicht deterministisch")
	}
}
