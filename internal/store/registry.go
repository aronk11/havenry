package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Opener öffnet ein Backend anhand einer DSN.
type Opener func(ctx context.Context, dsn string) (Full, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Opener{}
)

// Register meldet ein Backend für ein DSN-Schema an (ADR-0031).
//
// Wird im init() des jeweiligen Backend-Pakets aufgerufen. Ein Nebeneffekt im
// init() ist hier vertretbar, weil er genau eine Sache tut und nichts Fremdes
// verändert — anders als die frühere Lösung, bei der zwei init()-Funktionen
// eine geteilte Migrationsliste befüllten und die Reihenfolge zählte.
//
// Doppelte Registrierung ist ein Programmierfehler und keine Laufzeitfrage,
// deshalb Panik statt Fehlerwert.
func Register(scheme string, open Opener) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[scheme]; exists {
		panic("store: backend für schema " + scheme + " ist bereits registriert")
	}
	registry[scheme] = open
}

// Backends nennt die verfügbaren Schemata.
func Backends() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	out := make([]string, 0, len(registry))
	for s := range registry {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Open öffnet den Store anhand der DSN.
//
// Beispiele:
//
//	sqlite:///var/lib/havenry/havenry.db
//	postgres://havenry@db.local/havenry
//
// Ein blanker Pfad ohne Schema wird als SQLite gelesen — das war die einzige
// Möglichkeit, bevor es mehrere gab, und soll weiter funktionieren.
func Open(ctx context.Context, dsn string) (Full, error) {
	scheme, rest := splitDSN(dsn)

	registryMu.RLock()
	open, ok := registry[scheme]
	registryMu.RUnlock()

	if !ok {
		avail := Backends()
		if len(avail) == 0 {
			// Der klassische Fall: Das Backend-Paket wurde nicht importiert.
			// Ohne diesen Hinweis sucht man lange an der falschen Stelle.
			return nil, fmt.Errorf(
				"store: kein backend registriert — wird das backend-paket importiert? (blank import)")
		}
		return nil, fmt.Errorf("store: unbekanntes backend %q (verfügbar: %s)",
			scheme, strings.Join(avail, ", "))
	}
	return open(ctx, rest)
}

// splitDSN trennt Schema und Rest. Ohne Schema gilt sqlite.
func splitDSN(dsn string) (scheme, rest string) {
	if i := strings.Index(dsn, "://"); i > 0 {
		return dsn[:i], dsn[i+3:]
	}
	return "sqlite", dsn
}
