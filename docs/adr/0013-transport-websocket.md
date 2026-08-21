# ADR-0013 — WebSocket als Agent-Transport

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Aus ADR-0003 folgt eine dauerhafte, agent-initiierte Verbindung. Kandidaten: WebSocket,
gRPC-Bidi-Stream, langlebiges HTTP/2-SSE plus separater Command-Kanal.

## Entscheidung
**WebSocket** mit JSON-Nachrichten, versioniertes Nachrichtenschema, Heartbeat alle 20 s,
Reconnect mit exponentiellem Backoff und Jitter.

## Begründung
- Kommt durch Reverse-Proxies und Corporate-Proxies, mit denen gRPC oft kämpft.
- Ein Kanal für Reports, Kommandos und Log-Streams — keine zweite Verbindung.
- Debuggbar mit Standardwerkzeugen; für Beitragende leicht zugänglich.
- JSON kostet Bandbreite, aber bei 1–20 Hosts ist das irrelevant. Falls es je relevant
  wird, ist der Wechsel auf ein Binärformat eine interne Änderung.

## Nachrichtentypen (v1)
`hello` · `report.state` · `report.metrics` · `cmd.request` · `cmd.result` ·
`log.subscribe` · `log.chunk` · `ping`/`pong`

## Konsequenzen
- **Alle Kommandos müssen idempotent und mit einer `cmd_id` versehen sein** —
  Verbindungsabbrüche sind Normalzustand, Kommandos können doppelt ankommen.
- Kommandos haben ein serverseitiges Timeout und einen Endzustand; kein Kommando
  bleibt unbestimmt hängen.
