package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aronk11/havenry/internal/store"
)

// Die Registry entscheidet, welches Backend anläuft. Ein Fehler hier führt
// dazu, dass jemand mit einer falschen Datenbank startet und es erst merkt,
// wenn Daten fehlen.

func TestOpenWithoutBackendExplainsItself(t *testing.T) {
	// Im reinen store-Paket ist kein Backend registriert — der häufigste
	// Anfängerfehler ist ein vergessener blank import.
	_, err := store.Open(context.Background(), "sqlite:///tmp/x.db")
	if err == nil {
		t.Fatal("Open ohne registriertes Backend sollte scheitern")
	}
	if !strings.Contains(err.Error(), "import") {
		t.Errorf("Fehlermeldung nennt die Ursache nicht: %v", err)
	}
}

func TestRegisterAndOpen(t *testing.T) {
	called := ""
	store.Register("testbackend", func(_ context.Context, dsn string) (store.Full, error) {
		called = dsn
		return nil, nil
	})

	if _, err := store.Open(context.Background(), "testbackend://host/db"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Das Schema wird abgeschnitten, der Rest unverändert durchgereicht.
	if called != "host/db" {
		t.Fatalf("DSN an das Backend = %q, erwartet %q", called, "host/db")
	}

	var found bool
	for _, b := range store.Backends() {
		if b == "testbackend" {
			found = true
		}
	}
	if !found {
		t.Errorf("Backends() = %v, sollte testbackend enthalten", store.Backends())
	}
}

// TestBareePathIsSQLite: Vor der Registry gab es nur einen Pfad. Bestehende
// Aufrufe müssen weiter funktionieren.
func TestBarePathIsSQLite(t *testing.T) {
	got := ""
	store.Register("sqlite", func(_ context.Context, dsn string) (store.Full, error) {
		got = dsn
		return nil, nil
	})

	if _, err := store.Open(context.Background(), "/var/lib/havenry/havenry.db"); err != nil {
		t.Fatalf("blanker Pfad: %v", err)
	}
	if got != "/var/lib/havenry/havenry.db" {
		t.Fatalf("DSN = %q — ein blanker Pfad muss unverändert bei sqlite ankommen", got)
	}
}

func TestUnknownBackendListsAvailable(t *testing.T) {
	store.Register("bekannt", func(context.Context, string) (store.Full, error) { return nil, nil })

	_, err := store.Open(context.Background(), "gibtsnicht://x")
	if err == nil {
		t.Fatal("unbekanntes Backend sollte scheitern")
	}
	if !strings.Contains(err.Error(), "bekannt") {
		t.Errorf("Fehlermeldung nennt die verfügbaren Backends nicht: %v", err)
	}
}
