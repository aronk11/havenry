package controlplane_test

import (
	"bytes"
	"net/http"
	"testing"
)

// TestMutatingEndpointRejectsNonJSONContentType schließt die klassische
// JSON-CSRF-Lücke: Ein <form enctype="text/plain"> kann im Browser nur
// application/x-www-form-urlencoded, multipart/form-data oder text/plain als
// Content-Type erzwingen — nie application/json. Verlangt der Server exakt
// application/json, kann ein fremdes Formular niemals als gültige Anfrage
// durchgehen, unabhängig davon, was SameSite erlaubt (nötig geworden, weil
// die Sitzungs-Cookies für die Console jetzt SameSite=None brauchen).
func TestMutatingEndpointRejectsNonJSONContentType(t *testing.T) {
	_, ts, st := newTestServer(t)
	adminPassword(t, st, "admin-passwort-lang-genug")

	admin := &apiClient{t: t, base: ts.URL}
	admin.login("admin", "admin-passwort-lang-genug")

	body := []byte(`{"username":"eindringling","password":"sehr-langes-passwort","role":"admin"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/users", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	// So sähe die Anfrage aus einem <form enctype="text/plain"> aus — kein
	// echter Browser-Fetch, kein CORS-Preflight, aber mit dem Session-Cookie.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.AddCookie(admin.cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Content-Type text/plain = %d, erwartet 400 — sonst wäre ein Admin-Anlegen "+
			"per fremdem Formular möglich", resp.StatusCode)
	}
}
