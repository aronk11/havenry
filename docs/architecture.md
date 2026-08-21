# Architektur

## Überblick

```
                         ┌──────────────────────────────┐
    Git-Repo ──────────► │        Control Plane         │ ◄──── Web-UI  (v0.4: Mobile)
    (compose.yaml)       │                              │
                         │  gitsync   →  reconcile      │
                         │  store (SQLite)              │
                         │  transport (WebSocket-Hub)   │
                         │  api /api/v1                 │
                         └──────────────┬───────────────┘
                                        │ TLS, agent-initiiert (outbound)
                    ┌───────────────────┼───────────────────┐
                    ▼                   ▼                   ▼
              ┌──────────┐        ┌──────────┐        ┌──────────┐
              │  Agent   │        │  Agent   │        │  Agent   │
              │  docker  │        │  docker  │        │  docker  │
              │  .sock   │        │  .sock   │        │  .sock   │
              └──────────┘        └──────────┘        └──────────┘
```

## Datenfluss: Drift-Erkennung

1. `gitsync` holt das Repo, erkennt Stacks nach Konvention (ADR-0014).
2. `reconcile` normalisiert den Soll-Zustand aus den Compose-Dateien.
3. Agenten melden periodisch ihren Ist-Zustand; `reconcile` normalisiert diesen ebenfalls.
4. Semantischer Vergleich beider normalisierter Formen erzeugt eine Drift-Liste.
5. Im Modus `observe` endet die Kette hier — die Drift wird gespeichert und angezeigt.
   Im Modus `apply` werden Kommandos erzeugt und an den zuständigen Agenten gestellt.

**Der kritische Teil ist Schritt 2 und 3.** Ein textueller Vergleich erzeugt
Falsch-positive (`nginx` vs. `nginx:latest`, Portnotationen, implizite Compose-Defaults,
generierte Netzwerknamen). Falsch-positive Drifts zerstören das Vertrauen sofort und sind
das größte Qualitätsrisiko des Produkts.

## Paketstruktur

| Pfad | Verantwortung |
|------|---------------|
| `cmd/agent` | Agent-Einstiegspunkt, Flags, Enrollment |
| `cmd/controlplane` | Control-Plane-Einstiegspunkt |
| `internal/transport` | WebSocket-Hub und -Client, Nachrichtenschema, Reconnect |
| `internal/provider` | `Provider`-Schnittstelle und Capability-Flags |
| `internal/provider/docker` | Docker-Implementierung (lesen und schreiben) |
| `internal/gitsync` | Repo klonen, pollen, Stacks erkennen |
| `internal/reconcile` | Normalisierung, semantischer Diff, Kommando-Erzeugung |
| `internal/store` | SQLite-Zugriff, Migrationen, Event-Log |
| `internal/controlplane` | HTTP-API, Auth, Orchestrierung |
| `internal/agent` | Agent-Schleife, Kommando-Ausführung, Log-Streaming |
| `api` | OpenAPI-Spezifikation, Nachrichtenschema |
| `web` | Web-UI-Quellen (Build wird eingebettet) |

## Nicht-funktionale Vorgaben

- Agent-Binary < 20 MB, Agent-RSS im Leerlauf < 30 MB (ADR-0005, in CI geprüft)
- Control-Plane-RSS bei 10 Hosts < 150 MB
- Erste sinnvolle Anzeige nach Agent-Verbindung < 5 s
- amd64 und arm64 gleichwertig unterstützt
- Kein Phone-Home (ADR-0018)
