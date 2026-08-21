# ADR-0004 — Drift wird angezeigt, nicht automatisch überschrieben

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Das Kernrisiko dieser Produktkategorie: Ein GitOps-Tool, das nachts das laufende
Experiment des Nutzers wegräumt, wird am nächsten Morgen deinstalliert. Gleichzeitig ist
unbemerkter Drift — "diese Änderung liegt seit acht Monaten undokumentiert auf Host 3" —
genau der Pain, den die Plattform lösen soll.

## Entscheidung
Jeder Stack hat einen Modus. Default ist `observe`:
- Drift wird erkannt und als lesbarer Diff angezeigt.
- Der Nutzer entscheidet pro Drift: **adopt** (Änderung als Commit ins Repo übernehmen)
  oder **revert** (Ist-Zustand auf Git-Stand zurücksetzen).
- Es passiert nichts automatisch.

`apply` (kontinuierliches Angleichen an Git) ist opt-in pro Stack.

## Alternativen
- **Immer apply, wie klassisches GitOps:** korrekt im Enterprise, falsch im Homelab.
- **Nur anzeigen, kein adopt/revert:** halbe Lösung, Nutzer muss weiter manuell committen.

## Konsequenzen
- Der Modus-Schalter ist das differenzierende Produktmerkmal gegenüber Portainer,
  Dockge und klassischen GitOps-Tools. Er gehört auf die Landingpage.
- **adopt** braucht Schreibzugriff aufs Repo (Branch + Commit). Deploy-Key mit
  Schreibrecht ist optional; ohne ihn ist nur `revert` und Copy-to-Clipboard verfügbar.
- Drift-Erkennung muss semantisch sein, nicht textuell: `image: nginx` vs.
  `image: nginx:latest` ist kein Drift.
