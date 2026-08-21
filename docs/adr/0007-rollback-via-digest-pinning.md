# ADR-0007 — Rollback über Image-Digest-Pinning

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
"Das Update hat meinen Service zerlegt" ist einer der meistgenannten Homelab-Pains.
Bestehende Auto-Update-Tools aktualisieren, aber bieten keinen Weg zurück.

## Entscheidung
- Vor jedem Update wird der aktuell laufende Image-Digest festgehalten und gespeichert.
- Update = Digest-Wechsel, gefolgt von einem Health-Fenster (konfigurierbar, Default 120 s).
- Bleibt der Container im Health-Fenster nicht stabil oder drückt der Nutzer Rollback,
  wird der vorherige Digest wiederhergestellt.
- Die letzten N Digests pro Service bleiben in der Historie sichtbar.

## Alternativen
- **Tag-basiertes Rollback:** unzuverlässig, Tags sind veränderlich.
- **Volume-Snapshots vor Update:** löst auch Datenmigrationen, braucht aber
  Dateisystem-Unterstützung. Kandidat für v0.2 zusammen mit Backup.

## Konsequenzen
- **Ehrliche Grenze:** Datenmigrationen innerhalb des Containers macht ein Digest-Rollback
  nicht rückgängig. Das muss in der UI vor dem Update sichtbar stehen, nicht im Kleingedruckten.
- Erfordert Digest-Auflösung gegen die Registry; Rate-Limits (Docker Hub) einplanen und cachen.
