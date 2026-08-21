# ADR-0008 — Provider-Interface statt Plugin-System

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Erweiterbarkeit ist gefordert. Ein echtes Plugin-System (dynamisches Laden, stabile
öffentliche API, Versionierung) im MVP ist verfrühte Abstraktion und bindet Aufwand,
solange niemand Plugins schreibt.

## Entscheidung
Interne Schnittstellen werden von Anfang an sauber geschnitten, aber bleiben intern:

- `Provider` — Ist-Zustand lesen, Aktionen ausführen (Docker heute, Podman/Proxmox später)
- `SourceProvider` — Soll-Zustand liefern (Git heute)
- `BackupTarget` — v0.2
- `Notifier` — v0.2 (Push, Webhook, ntfy, E-Mail)
- `NetworkExposer` — v0.3 (Cloudflare Tunnel, Tailscale)

Eine Schnittstelle wird erst öffentlich stabilisiert, wenn **zwei** echte
Implementierungen existieren. Vorher darf sie sich frei ändern.

## Konsequenzen
- Erweiterbar ohne den Wartungspreis eines Plugin-Systems.
- Die Zwei-Implementierungen-Regel verhindert Schnittstellen, die um genau einen
  Anwendungsfall herum entworfen wurden.
