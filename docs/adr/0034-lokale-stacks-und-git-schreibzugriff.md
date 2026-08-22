# ADR-0034 — Lokale Stacks, Git-Schreibzugriff und direktes Container-Management

**Status:** Akzeptiert · **Datum:** 2026-08-22

## Kontext

ADR-0002 legt fest: Git ist die *einzige* Quelle der Wahrheit, die Datenbank ist
jederzeit löschbar. Das ist für Nutzer richtig, die ohnehin GitOps betreiben.
Es ist eine harte Hürde für alle anderen — wer nur schnell einen Stack
ausprobieren will, muss zuerst ein Repo anlegen, einen Deploy-Key einrichten
und einen Commit schreiben, bevor Havenry überhaupt etwas anzeigt.

Der Wunsch (Nutzer-Feedback): drei Wege sollen nebeneinander funktionieren.

1. Git-Repo anbinden — bestehend, bleibt der empfohlene Weg für alle, die
   Versionierung und Review wollen. Perspektivisch mit Schreibzugriff, damit
   Havenry z. B. Digest-Updates zurück committen kann — nicht nur für GitHub,
   sondern plattformübergreifend, soweit möglich (GitHub, GitLab, gitea,
   selbstgehostet). Das bleibt bei SSH-Deploy-Keys als kleinstem gemeinsamen
   Nenner (ADR-0021); eine GitHub App ist eine spätere, GitHub-spezifische
   *Ergänzung* für Auto-Sync per Webhook und feingranulare Schreibrechte,
   kein Ersatz.
2. Compose-Dateien direkt in Havenry pflegen — ohne dass irgendwo ein
   Git-Repo existiert.
3. Container direkt erstellen und verwalten — imperativ, ohne Compose-Datei
   im Hintergrund.

## Entscheidung

Havenry bekommt einen zweiten, klar gekennzeichneten Stack-Typ: **lokale
Stacks**. Sie werden in der Datenbank gespeichert (`LocalStack` in
`internal/store`) statt aus einem Git-Checkout gelesen, laufen aber durch
denselben Ausführungspfad wie git-verwaltete Stacks: `applyStack()` sendet
`ComposeYAML` unverändert an den Agenten (ADR-0027 hat den Agenten von Anfang
an contentbasiert gebaut, nicht pfadbasiert — dafür war keine Änderung am
Agenten nötig).

Das ist eine bewusste Ausnahme von ADR-0002: Für lokale Stacks *ist* die
Datenbank die einzige Kopie. Deshalb:

- Jede lokale Stack-Ansicht in der Oberfläche macht sichtbar, dass es **kein**
  Backup außer der Havenry-Datenbank gibt — kein verstecktes Sicherheitsnetz,
  keine Behauptung, es sei "wie Git, nur bequemer".
- Drift-Erkennung (ADR-0004, ADR-0026) gilt weiter: Ist-Zustand wird mit dem
  gespeicherten Compose-Inhalt verglichen, genau wie bei git-Stacks.
- `adopt` (ADR-0028) schreibt bei lokalen Stacks direkt in die Datenbank statt
  einen Commit zu erzeugen — es gibt kein Repo, das einen Commit haben könnte.

Direktes Container-Management (Punkt 3) und Git-Schreibzugriff samt GitHub App
(Punkt 1, Erweiterung) werden hier nur benannt, nicht in dieser ADR entschieden
— das sind eigene Entscheidungen mit eigenem Umfang. Reihenfolge der Umsetzung
laut Nutzer: lokale Stacks zuerst, dann direktes Container-Management, GitHub
App zuletzt (größter Infrastrukturaufwand: App-Registrierung, Installationsfluss,
Webhook-Endpunkt, Token-Handling).

## Alternativen

- **Nichts ändern, nur Git.** Verworfen — genau die Hürde, die den Wunsch
  ausgelöst hat, bliebe bestehen.
- **Lokale Stacks als Datei statt in der DB** (z. B. ein von Havenry selbst
  verwaltetes, verstecktes Git-Repo). Verworfen für den ersten Wurf: deutlich
  mehr Komplexität (eigenes Repo pflegen, committen, ggf. Merge-Konflikte mit
  sich selbst) für einen Nutzen, den die Datenbank genauso erfüllt. Bleibt eine
  Option, falls Versionshistorie für lokale Stacks später gewünscht wird.

## Konsequenzen

- Das Store-Interface wächst um `LocalStackStore` (memstore + sqlitestore).
- `docs/status.md` und das README müssen an der Stelle "Git als Source of
  Truth" präzisiert werden: Git ist der *empfohlene* Weg für Versionierung und
  Review, nicht mehr der einzige unterstützte.
- Wer nur lokale Stacks nutzt, verliert den zentralen Verkaufsgrund "kein
  Lock-in, deinstallier das Tool und dein Setup läuft weiter" für genau diese
  Stacks — das muss in der Oberfläche ehrlich stehen, nicht kleingedruckt.
