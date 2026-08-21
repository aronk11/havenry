# ADR-0027 — Compose-Ausführung über die Docker-CLI

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
`revert` (ADR-0004) und der Modus `apply` brauchen die Fähigkeit, einen Stack in
den beschriebenen Zustand zu bringen. Bisher spricht der Agent nur die Docker
Engine API (ADR-0001) — die kennt einzelne Container, aber kein Compose.

Zwei Wege:
1. Compose-Semantik selbst über die Engine API nachbilden.
2. `docker compose` als Unterprozess aufrufen.

## Entscheidung
Der Agent ruft `docker compose` auf.

## Begründung
Compose selbst nachzubauen hieße, Abhängigkeitsreihenfolge, Netzwerk- und
Volume-Erzeugung, Namensgebung, `depends_on`, Health-Bedingungen, Profile und
Merge-Regeln nachzuimplementieren — und bei jeder Compose-Version nachzuziehen.
Jede Abweichung vom Original würde als Fehler erscheinen, den niemand
nachvollziehen kann, weil dieselbe Datei mit `docker compose up` funktioniert.

Das ist dieselbe Überlegung wie bei Git (ADR-0021): Das Werkzeug, das der Nutzer
ohnehin kennt und benutzt, verhält sich garantiert richtig.

## Umsetzung
- Die Control Plane schickt den **Inhalt** der Compose-Datei mit dem Kommando.
  Der Agent braucht keinen Zugriff auf das Repo — das bleibt allein bei der
  Control Plane.
- Der Agent legt die Datei unter `<state-dir>/stacks/<stack>/compose.yaml` ab
  und ruft `docker compose -p <stack> -f <datei> up -d --remove-orphans` auf.
- Vorhandene `.env`-Dateien auf dem Host bleiben unangetastet und werden
  weiterverwendet (ADR-0006) — sie liegen im selben Verzeichnis.
- Aufrufe laufen mit Zeitlimit und ohne Shell.
- `docker compose` wird beim Agent-Start geprüft. Fehlt es, meldet der Agent
  eingeschränkte Fähigkeiten (`CapApply` fehlt), statt Kommandos später
  unerklärt scheitern zu lassen.

## Konsequenzen
- **`docker compose` (Plugin v2) muss auf jedem Host vorhanden sein**, der
  `revert` oder `apply` nutzen soll. Für reine Beobachtung genügt weiterhin der
  Docker-Socket allein.
- Der Agent schreibt jetzt Dateien außerhalb seines Credential-Verzeichnisses.
  Der Pfad wird aus dem Stack-Namen gebildet und dabei geprüft — ein Name aus
  dem Repo darf nicht aus dem Verzeichnis herausführen.
- Das Ergebnis eines Aufrufs wird vollständig durchgereicht: Bei einem Fehler
  sieht der Nutzer die Ausgabe von `docker compose`, nicht unsere Umschreibung.
