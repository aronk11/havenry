package controlplane

import (
	"net/http"
	"testing"
)

// TestSessionSameSiteAllowsCrossOrigin belegt den Grund für die Wahl in
// auth.go: Eine getrennt ausgelieferte Console (ADR-0032) kann sich mit
// SameSite=Lax nie anmelden, weil Browser Lax-Cookies bei einem per
// JavaScript ausgelösten Cross-Origin-fetch() gar nicht erst mitschicken.
func TestSessionSameSiteAllowsCrossOrigin(t *testing.T) {
	if got := sessionSameSite(true); got != http.SameSiteNoneMode {
		t.Fatalf("sessionSameSite(true) = %v, erwartet SameSiteNoneMode — sonst kann eine getrennt "+
			"ausgelieferte Console sich nie anmelden", got)
	}
	// Ohne TLS würde ein Browser SameSite=None ohnehin verwerfen (er verlangt
	// dafür Secure) — Lax ist hier der einzig funktionierende Rückfall,
	// solange man sich auf denselben Ursprung beschränkt.
	if got := sessionSameSite(false); got != http.SameSiteLaxMode {
		t.Fatalf("sessionSameSite(false) = %v, erwartet SameSiteLaxMode", got)
	}
}
