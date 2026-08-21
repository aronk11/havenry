# ADR-0002 — Git ist Source of Truth, die DB ist abgeleitet

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Der stärkste Vertrauensanker in dieser Community ist Portabilität. Tools, die den
Zustand in eine proprietäre Datenbank sperren, werden misstrauisch beäugt — zu Recht.

## Entscheidung
Der gewünschte Zustand lebt ausschließlich als unveränderte `compose.yaml`-Dateien im
Git-Repository des Nutzers. Die Datenbank der Control Plane enthält nur abgeleiteten
Zustand: Ist-Zustand der Hosts, Events, Metrik-Cache, Sessions. Sie ist jederzeit
löschbar und wird aus Git plus Agent-Reports vollständig neu aufgebaut.

## Alternativen
- **Eigenes Config-Format:** mächtiger, aber Lock-in und Lernkosten.
- **DB als Wahrheit, Git als Export:** bequemer zu bauen, zerstört das Kernversprechen.

## Konsequenzen
- Nutzer können jederzeit am Repo vorbei arbeiten — die Plattform muss damit umgehen (ADR-0004).
- Deinstallation hinterlässt ein voll funktionsfähiges Setup. Das ist das Marketingargument.
- Keine Features, die sich nicht in Compose ausdrücken lassen. Bewusste Grenze.
