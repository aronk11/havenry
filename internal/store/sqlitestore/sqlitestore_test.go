package sqlitestore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/store/sqlitestore"
	"github.com/aronk11/havenry/internal/store/storetest"
)

// Die gesamte Zusage des Store steckt in der Suite. Ein neues Backend braucht
// genau diese Datei mit einer anderen Zeile in der Factory (ADR-0031).
func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Full {
		s, err := sqlitestore.OpenSQLite(context.Background(),
			filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatalf("store öffnen: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

// TestPersistsAcrossRestart ist SQLite-eigen: Es prüft, dass die Datei nach
// dem Schließen und erneuten Öffnen noch dasselbe enthält. Ein Backend mit
// Netzwerkdatenbank hätte hier eine andere Prüfung.
func TestPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "persist.db")

	s1, err := sqlitestore.OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.UpsertHost(ctx, store.Host{
		ID: "host-1", Hostname: "nas-01", CredentialHash: "cred-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s1.ApproveHost(ctx, "host-1"); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := sqlitestore.OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatalf("erneutes Öffnen: %v", err)
	}
	defer s2.Close() //nolint:errcheck

	h, err := s2.HostByCredentialHash(ctx, "cred-1")
	if err != nil {
		t.Fatalf("Host nach Neustart nicht gefunden: %v", err)
	}
	if !h.Approved {
		t.Fatal("Bestätigung ging beim Neustart verloren")
	}
}

// TestMigrationsAreIdempotent: Mehrfaches Öffnen derselben Datei darf weder
// scheitern noch Daten verändern.
func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "migrate.db")

	for i := 0; i < 3; i++ {
		s, err := sqlitestore.OpenSQLite(ctx, dsn)
		if err != nil {
			t.Fatalf("Öffnen Nr. %d: %v", i+1, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
