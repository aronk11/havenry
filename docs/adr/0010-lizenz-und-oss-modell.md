# ADR-0010 — Open-Source-Core, Lizenzwahl

**Status:** Akzeptiert · **Datum:** 2026-08-20

## Kontext
In dieser Community ist ein Open-Source-Core praktisch Voraussetzung für organisches
Wachstum. Monetarisierung funktioniert über Convenience (gehostet, Mobile-Pro,
Team-Features), nicht über Paywalls im Kern. Die Lizenz ist im Nachhinein nur schwer
änderbar, sobald externe Beiträge existieren.

## Optionen

**A — Apache-2.0**
Maximale Verbreitung und Beitragsbereitschaft. Erlaubt aber Dritten, eine gehostete
Version anzubieten, ohne etwas zurückzugeben.

**B — AGPL-3.0**
Schützt gegen Cloud-Weiterverwertung durch Dritte. Schreckt manche Firmennutzer ab und
kann Beiträge dämpfen.

**C — AGPL-3.0 + CLA**
Schutz plus die Option auf spätere kommerzielle Lizenzierung. Ein CLA ist in
Hobby-Communities allerdings ein bekannter Beitrags-Dämpfer.

## Entscheidung
**Option B: AGPL-3.0, ohne CLA.**

Begründung: Der Schutz gegen Weiterverwertung ist real und kostet hier wenig —
die Zielgruppe sind Einzelpersonen und kleine Teams, für die die AGPL keine
Hürde darstellt. Firmen, deren Richtlinien AGPL ausschließen, gehören ohnehin
nicht zur Zielgruppe (ADR-0001 schließt das Enterprise-Segment bewusst aus).

**Kein CLA.** Das ist die bewusste Kehrseite: Sobald externe Beiträge
eingegangen sind, ist eine spätere Umlizenzierung nur mit Zustimmung aller
Beitragenden möglich. Dafür bleibt die Beitragshürde bei null — und in einem
Projekt, das von Community-Wachstum lebt, wiegt das schwerer als die theoretische
Option auf einen Lizenzwechsel.

**Solange du Alleinautor bist, ist die Entscheidung umkehrbar.** Ein Wechsel auf
Apache-2.0 ist bis zum ersten fremden Commit jederzeit möglich.

## Folgen für die Kommerzialisierung
Die AGPL schließt ein späteres kommerzielles Angebot nicht aus, sondern
schützt es: Als Rechteinhaber kannst du dein eigenes Werk zusätzlich unter
anderen Bedingungen anbieten — Dritte, die eine gehostete Fassung verkaufen
wollen, müssen ihre Änderungen offenlegen.

Die naheliegenden Kandidaten bleiben: gehostete Control Plane (löst das
Erreichbarkeitsproblem aus ADR-0003), Push-Benachrichtigungen für die mobilen
Apps, Team-Funktionen.

## Umgesetzt
- `LICENSE` mit dem vollständigen AGPL-3.0-Text
- Quelltext-Endpunkt `/source` und Verweis in der Oberfläche (AGPL §13) — Pflicht,
  sobald die Oberfläche über ein Netz erreichbar ist
