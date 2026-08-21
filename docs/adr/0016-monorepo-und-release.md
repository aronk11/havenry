# ADR-0016 — Repositories, Versionierung und Release-Prozess

**Status:** Teilweise ersetzt durch [ADR-0032](0032-repo-aufteilung.md) · **Datum:** 2026-08-19

## Kontext
Agent, Control Plane, Web-UI und später zwei mobile Clients. Getrennte Repos bedeuten
Versions-Matrix-Schmerz bei einem Ein-Personen-Projekt.

## Entscheidung
- **Ein Monorepo** für Agent, Control Plane, Web-UI und Dokumentation. Mobile Clients
  kommen ab v0.4 in eigene Repos, da eigene Toolchains und Release-Zyklen.
- **Eine gemeinsame Version** für Agent und Control Plane. Der Agent verweigert die
  Arbeit bei inkompatibler Protokollversion und meldet das lesbar in der UI.
- **Conventional Commits** plus automatisches Release (Tag, Changelog, Binaries,
  Container-Images für amd64 und arm64 — arm64 ist in dieser Zielgruppe Pflicht, nicht Kür).
- Protokollversion wird **getrennt** von der Produktversion geführt und nur bei
  Breaking Changes erhöht.

## Konsequenzen
- Ein Tag, ein Release, keine Kompatibilitätsmatrix.
- Nutzer müssen Agenten mit-updaten. Die UI zeigt veraltete Agenten prominent an;
  Agent-Selbstupdate ist ein Kandidat für v0.2.
