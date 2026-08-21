# ADR-0017 — Web-UI-Stack und Einbettung ins Binary

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Aus ADR-0005 folgt: ein Binary. Die UI ist ein Dashboard — Listen, Diffs, Log-Streams,
ein paar Aktionen. Kein komplexer Client-State.

## Entscheidung
- Schlankes SPA, gebaut mit einem gängigen Toolchain, per `embed` ins Go-Binary gepackt.
- Kommunikation ausschließlich über die dokumentierte `/api/v1` (ADR-0009).
- Live-Updates über eine WebSocket-Verbindung Browser → Control Plane.
- Kein SSR, kein Meta-Framework, keine Laufzeit-Abhängigkeit zu einem Node-Prozess.

## Konsequenzen
- Kein separates Frontend-Deployment; ein Container liefert alles aus.
- Die UI ist austauschbar, weil sie keine Sonderrechte hat — eine Community-Alternative
  oder ein CLI-Client ist jederzeit möglich.
- Build braucht Node zur Bauzeit, nicht zur Laufzeit. CI-Artefakt bleibt ein Binary.
