package controlplane_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/aronk11/havenry/internal/store"
)

const validCompose = "services:\n  web:\n    image: nginx:1.27\n"

// TestLocalStackCRUDOverHTTP prüft den vollen Weg über die API — nicht nur
// den Store direkt, aus demselben Grund wie in api_auth_test.go: eine Route,
// die requireAuth vergisst, fällt sonst nicht auf.
func TestLocalStackCRUDOverHTTP(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")
	ctx := context.Background()

	if err := st.UpsertHost(ctx, store.Host{ID: "host-1", Hostname: "nas-01", CredentialHash: "c1"}); err != nil {
		t.Fatal(err)
	}

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")

	// Anlegen mit ungültigem Compose-Inhalt schlägt fehl — nicht erst beim
	// Anwenden auffallen lassen, was schon beim Speichern erkennbar ist.
	resp, out := admin.do("POST", "/api/v1/hosts/host-1/local-stacks", map[string]any{
		"name": "kaputt", "compose_yaml": "das ist kein yaml: [",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ungültiges compose anlegen = %d %v, erwartet 400", resp.StatusCode, out)
	}

	resp, out = admin.do("POST", "/api/v1/hosts/host-1/local-stacks", map[string]any{
		"name": "app", "compose_yaml": validCompose,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("anlegen = %d %v", resp.StatusCode, out)
	}
	if out["name"] != "app" {
		t.Errorf("name in antwort = %v", out["name"])
	}

	// Doppelter Name auf demselben Host wird abgelehnt.
	resp, _ = admin.do("POST", "/api/v1/hosts/host-1/local-stacks", map[string]any{
		"name": "app", "compose_yaml": validCompose,
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("doppelter name = %d, erwartet 409", resp.StatusCode)
	}

	// Liste enthält genau einen Eintrag.
	resp, out = admin.do("GET", "/api/v1/hosts/host-1/local-stacks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("liste = %d", resp.StatusCode)
	}
	stacks, _ := out["local_stacks"].([]any)
	if len(stacks) != 1 {
		t.Fatalf("local_stacks = %d einträge, erwartet 1: %v", len(stacks), out)
	}

	// Bearbeiten übernimmt den neuen Inhalt.
	resp, out = admin.do("PUT", "/api/v1/hosts/host-1/local-stacks/app", map[string]any{
		"compose_yaml": "services:\n  web:\n    image: nginx:1.28\n",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearbeiten = %d %v", resp.StatusCode, out)
	}
	if out["compose_yaml"] != "services:\n  web:\n    image: nginx:1.28\n" {
		t.Errorf("compose_yaml nach update = %v", out["compose_yaml"])
	}

	// Anwenden ohne verbundenen Host scheitert kontrolliert (kein Absturz,
	// kein hängender Request) statt eines internen Fehlers.
	resp, _ = admin.do("POST", "/api/v1/hosts/host-1/local-stacks/app/apply", nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("anwenden ohne verbundenen host = %d, erwartet 502", resp.StatusCode)
	}

	// Löschen entfernt den Eintrag, ein zweites Löschen meldet 404.
	resp, _ = admin.do("DELETE", "/api/v1/hosts/host-1/local-stacks/app", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("löschen = %d", resp.StatusCode)
	}
	resp, _ = admin.do("DELETE", "/api/v1/hosts/host-1/local-stacks/app", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("erneutes löschen = %d, erwartet 404", resp.StatusCode)
	}
}

// TestViewerCannotManageLocalStacks spiegelt TestViewerCannotControlContainers:
// Lesen ist erlaubt, Anlegen/Ändern/Löschen ist admin-only (ADR-0034).
func TestViewerCannotManageLocalStacks(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")
	ctx := context.Background()

	if err := st.UpsertHost(ctx, store.Host{ID: "host-1", Hostname: "nas-01", CredentialHash: "c1"}); err != nil {
		t.Fatal(err)
	}

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")
	if resp, _ := admin.do("POST", "/api/v1/users", map[string]any{
		"username": "gast", "password": "gast-passwort-lang-genug",
		"role": "viewer", "host_ids": []string{},
	}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("nutzer anlegen fehlgeschlagen: %d", resp.StatusCode)
	}

	gast := &apiClient{t: t, base: ts.URL}
	gast.login("gast", "gast-passwort-lang-genug")

	if resp, _ := gast.do("GET", "/api/v1/hosts/host-1/local-stacks", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("viewer darf lokale stacks lesen: %d, erwartet 200", resp.StatusCode)
	}
	if resp, _ := gast.do("POST", "/api/v1/hosts/host-1/local-stacks", map[string]any{
		"name": "app", "compose_yaml": validCompose,
	}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer konnte einen lokalen stack anlegen: %d, erwartet 403", resp.StatusCode)
	}
}

// TestLocalStackHostScopeIsEnforced: ein unbekannter Host liefert 404, kein
// interner Fehler — konsistent mit requireHostAccess (ADR-0022: ein Nutzer
// ohne Zugriff soll nicht erfahren, dass es den Host überhaupt gibt).
func TestLocalStackHostScopeIsEnforced(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")

	resp, out := admin.do("POST", "/api/v1/hosts/gibt-es-nicht/local-stacks", map[string]any{
		"name": "app", "compose_yaml": validCompose,
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anlegen auf unbekanntem host = %d %v, erwartet 404", resp.StatusCode, out)
	}
}
