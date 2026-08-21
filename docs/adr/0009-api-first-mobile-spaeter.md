# ADR-0009 — API-first, native Apps erst nach Web-Validierung

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Native Apps sind ein echtes Differenzierungsmerkmal — vorhandene Alternativen in diesem
Feld sind Hobby-Projekte. Gleichzeitig sind sie teuer in Wartung, Review-Zyklen und
Plattform-Eigenheiten.

## Entscheidung
Die HTTP-API ist ein First-Class-Produkt, nicht ein Nebenprodukt der Web-UI: dokumentiert
(OpenAPI), versioniert (`/api/v1`), vollständig. Die Web-UI benutzt ausschließlich diese
API — kein privilegierter Zugang, keine Sonderendpunkte.

Native Apps (SwiftUI, Kotlin Compose) werden ab v0.4 gebaut, nach validierter Web-Nutzung.

## Konsequenzen
- Kein verschwendeter Aufwand, falls das Web-Produkt nicht zieht.
- Die API-Disziplin zahlt sich unabhängig aus: sie ermöglicht CLI, Homepage-Widgets und
  Community-Integrationen ohne Zusatzarbeit.
- Push-Benachrichtigungen brauchen später einen Relay-Dienst — Designentscheidung für v0.4,
  hat kommerzielle Relevanz (ADR-0010).
