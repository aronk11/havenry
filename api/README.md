# API und Nachrichtenschema

Zwei getrennte Verträge:

## `/api/v1` — HTTP-API (Control Plane ↔ Clients)
Versioniert, dokumentiert, vollständig. Die Web-UI benutzt ausschließlich diese API und
hat keine Sonderrechte (ADR-0009). Damit sind CLI, mobile Apps und Community-Clients
ohne Zusatzarbeit möglich.

→ `openapi.yaml` (entsteht in M2)

## Agent-Protokoll (Control Plane ↔ Agent)
WebSocket, JSON, eigene Versionierung getrennt von der Produktversion (ADR-0016).

Nachrichtentypen v1:

| Typ | Richtung | Zweck |
|-----|----------|-------|
| `hello` | Agent → CP | Version, Host-Identität, Capabilities |
| `report.state` | Agent → CP | Vollständiger Ist-Zustand |
| `report.metrics` | Agent → CP | CPU, RAM, Disk |
| `cmd.request` | CP → Agent | Kommando mit `cmd_id`, **muss idempotent sein** |
| `cmd.result` | Agent → CP | Endzustand des Kommandos |
| `log.subscribe` | CP → Agent | Log-Stream anfordern |
| `log.chunk` | Agent → CP | Log-Daten |
| `ping` / `pong` | beide | Heartbeat, 20 s |

→ `messages.md` (entsteht in M1)
