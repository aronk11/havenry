# ADR-0005 — Ein Binary, eingebettete DB, keine externen Dienste

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Jede zusätzliche Laufzeitabhängigkeit — Postgres, Redis, ein Message-Broker — kostet
Installationen. "Leichtgewichtig" ist eine explizite Produktvorgabe, kein Nice-to-have.

## Entscheidung
- Control Plane: ein Go-Binary mit eingebetteter SQLite-Datenbank und eingebetteter Web-UI.
- Agent: ein Go-Binary ohne Persistenz außer lokalem Cache.
- Kein externer Broker. Der Agent-Stream (ADR-0013) ist der Zustellweg.

**Budget (verbindlich, wird in CI geprüft):** Agent-Binary < 20 MB, Agent-RSS im Leerlauf
< 30 MB, Control-Plane-RSS bei 10 Hosts < 150 MB.

## Alternativen
- **Postgres als Default:** skaliert besser, kostet die Hälfte der potenziellen Nutzer.
- **Externer Broker (NATS):** elegant für Fan-out, unnötig bei 1–20 Hosts.

## Konsequenzen
- Installation in einem Befehl, ein Container, ein Volume.
- Skaliert nicht auf tausende Hosts — für die Zielgruppe irrelevant.
- SQLite-Schreiblast muss gebündelt werden (Batching der Agent-Reports), sonst wird die
  DB zum Flaschenhals. Metriken werden aggregiert, nicht roh persistiert (ADR-0018).
