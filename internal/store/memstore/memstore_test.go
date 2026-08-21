package memstore_test

import (
	"testing"

	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/store/memstore"
	"github.com/aronk11/havenry/internal/store/storetest"
)

// Dieselbe Suite, anderes Backend. Genau diese Datei ist alles, was ein neues
// Backend an Testcode braucht (ADR-0031).
func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Full {
		return memstore.New()
	})
}
