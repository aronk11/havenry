package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// CORS entscheidet, wer im Namen eines angemeldeten Nutzers handeln darf.
// Eine zu großzügige Regel wäre kein Komfortfehler, sondern eine offene Tür.

func TestCORSIsClosedByDefault(t *testing.T) {
	c := newCORS(nil)
	if c.enabled() {
		t.Fatal("ohne Angabe darf CORS nicht aktiv sein")
	}
	if c.allowed("https://irgendwas.example") {
		t.Fatal("ohne eingetragene Herkunft darf nichts erlaubt sein")
	}
}

func TestCORSExactMatchOnly(t *testing.T) {
	c := newCORS([]string{"https://console.example.com"})

	if !c.allowed("https://console.example.com") {
		t.Error("die eingetragene Herkunft wurde abgelehnt")
	}
	// Der abschließende Schrägstrich wird beim Eintragen entfernt, damit eine
	// Angabe mit und ohne ihn dasselbe bedeutet.
	if !c.allowed("https://console.example.com/") {
		t.Error("Schrägstrich am Ende führte zur Ablehnung")
	}

	// Alles andere ist fremd — besonders die Fälle, die nach Tippfehler
	// aussehen, aber Angriffe sind.
	for _, bad := range []string{
		"https://console.example.com.angreifer.test",
		"http://console.example.com", // anderes Schema
		"https://sub.console.example.com",
		"https://example.com",
		"null",
		"",
	} {
		if c.allowed(bad) {
			t.Errorf("fremde Herkunft %q wurde erlaubt", bad)
		}
	}
}

func TestCORSHeadersOnAllowedOrigin(t *testing.T) {
	c := newCORS([]string{"https://console.example.com"})
	h := c.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Errorf("Allow-Origin = %q", got)
	}
	// Ohne Credentials nützt die Erlaubnis nichts — die Sitzung steckt im Cookie.
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("Allow-Credentials fehlt")
	}
	// Ohne Vary liefert ein Zwischenspeicher die Kopfzeile der falschen Herkunft aus.
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("Vary = %q, erwartet Origin", rec.Header().Get("Vary"))
	}
}

func TestCORSNoHeadersForForeignOrigin(t *testing.T) {
	c := newCORS([]string{"https://console.example.com"})
	h := c.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
	req.Header.Set("Origin", "https://angreifer.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("einer fremden Herkunft wurde die Erlaubnis erteilt")
	}
}

func TestCORSPreflight(t *testing.T) {
	c := newCORS([]string{"https://console.example.com"})
	h := c.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("Preflight darf den Handler nicht erreichen")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/hosts", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Preflight = %d, erwartet 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Allow-Methods fehlt")
	}
}

// TestCORSNeverAllowsWildcard: Mit Credentials verbietet der Standard "*"
// ohnehin — der Test hält fest, dass wir es auch nicht versuchen.
func TestCORSNeverAllowsWildcard(t *testing.T) {
	c := newCORS([]string{"*"})
	if c.allowed("https://beliebig.test") {
		t.Fatal("ein Stern wurde als Platzhalter gedeutet")
	}
}
