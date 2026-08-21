# ADR-0032 — Aufteilung in Repositories und Lizenzschichten

**Status:** Akzeptiert · **Datum:** 2026-08-20

## Kontext
Der Kern soll offen bleiben, die Oberfläche und spätere Geschäftsteile sollen
vorbehalten werden können. Die CLI soll öffentlich sein.

Das ist das klassische Open-Core-Muster. Es hat zwei Voraussetzungen, die vor
der ersten Zeile Code geklärt sein müssen — beide betreffen die AGPL.

## Der harte Teil zuerst: Was die AGPL erlaubt und was nicht

**Eine eingebettete Oberfläche ist Teil desselben Programms.** Die Web-UI liegt
derzeit per `go:embed` im Binary. Eine solche Oberfläche kann nicht proprietär
sein, solange der Kern AGPL ist — sie wird mit ihm ausgeliefert und ist
unstrittig dasselbe Werk.

**Ein getrenntes Programm über eine dokumentierte Schnittstelle ist ein anderer
Fall.** Eine Oberfläche, die eigenständig ausgeliefert wird und ausschließlich
über die HTTP-API spricht, gilt weithin als eigenes Werk. Dieser Weg wird von
vielen Open-Core-Projekten so gegangen. Er ist nicht risikofrei und die
Bewertung hängt vom Rechtsraum ab.

**Entscheidend ist aber etwas anderes: Wer die Rechte hält, ist an die eigene
Lizenz nicht gebunden.** Als alleiniger Urheber kannst du dein Werk zusätzlich
unter beliebigen Bedingungen anbieten. Die AGPL bindet Dritte, nicht dich.

## Der Konflikt mit ADR-0010

ADR-0010 hat **bewusst auf ein CLA verzichtet**, um die Beitragshürde niedrig zu
halten. Diese Entscheidung war für ein reines Community-Projekt richtig. Für
Open Core ist sie ein Problem:

**Sobald fremder Code im Kern liegt, gehört dir das Werk nicht mehr allein.**
Du kannst es dann weder umlizenzieren noch Teile davon proprietär anbieten,
ohne die Zustimmung jedes Beitragenden.

Das ist kein theoretisches Risiko, sondern der übliche Verlauf: Der erste
Pull Request kommt, man freut sich, man merged ihn.

**Zu entscheiden, bevor der erste fremde Beitrag angenommen wird:**

- **A — bei „kein CLA" bleiben.** Der Kern bleibt dauerhaft Gemeinschaftsgut.
  Proprietär kann nur werden, was du getrennt und selbst schreibst. Ehrlich,
  einfach, und die Tür zur Umlizenzierung fällt zu.
- **B — DCO plus CLA einführen, jetzt.** Beitragende treten dir Nutzungsrechte
  ab. Übliche Praxis bei Open Core, dämpft aber Beiträge und wirkt auf manche
  abschreckend.

Diese ADR entscheidet das nicht — es ist eine geschäftliche Frage, keine
technische. Sie benennt nur, dass sie ansteht und wann sie zu spät ist.

**Kein juristischer Rat.** Für den Schritt in die Kommerzialisierung ist eine
anwaltliche Prüfung das Geld wert.

## Entscheidung: drei Repositories

| Repository | Sichtbarkeit | Lizenz | Inhalt |
|---|---|---|---|
| `havenry` | öffentlich | AGPL-3.0 | Control Plane, Agent, Store, API, Marketing-Seite |
| `havenry-cli` | öffentlich | Apache-2.0 | `havenryctl` und der wiederverwendbare API-Client |
| `havenry-console` | privat | vorbehalten | React-Oberfläche für Teams und Verwaltung |

### Warum die CLI Apache-2.0 bekommt und nicht AGPL
Eine CLI wird eingebunden: in Skripte, in CI-Pipelines, in fremde Werkzeuge.
Unter AGPL müsste jeder, der den API-Client in sein Programm übernimmt, dieses
offenlegen. Das verhindert genau die Verbreitung, die eine CLI wertvoll macht.

Apache-2.0 ist hier kein Nachgeben, sondern der Zweck: Das Ding soll benutzt
werden. Es enthält keine Geschäftslogik, sondern spricht eine ohnehin
dokumentierte Schnittstelle.

### Warum die Console ein eigenes Repository ist
Nicht aus Ordnungsliebe. **Solange sie im Kern-Repository liegt, ist sie AGPL** —
unabhängig davon, was in einer Datei steht. Die Trennung ist die
Voraussetzung dafür, dass sie es nicht sein muss.

## Technische Folgen

**Der API-Client muss aus `internal/` heraus.** Go verbietet den Import von
`internal/` über Modulgrenzen hinweg. Der Client zieht mit der CLI um und wird
dort ein öffentliches Paket.

**Der Kern braucht CORS.** Eine getrennt ausgelieferte Oberfläche läuft unter
einer anderen Herkunft. Ohne ausdrücklich erlaubte Herkünfte kann sie die API
nicht ansprechen. Standardmäßig ist die Liste leer — eine offene
CORS-Regel wäre ein Loch, kein Feature.

**Die mitgelieferte Oberfläche bleibt im Kern und bleibt AGPL.** Sie wird nicht
entfernt: Ein Kern, der ohne proprietäres Zubehör keine Oberfläche hat, ist
kein Open-Source-Projekt, sondern ein Köder.

## Konsequenzen
- Drei Repositories statt einem. Eine Änderung an der API betrifft jetzt
  möglicherweise drei Stellen — deshalb ist die Versionierung aus ADR-0030
  Voraussetzung und nicht Beiwerk.
- Der Kern muss ohne Console vollständig benutzbar bleiben. Sobald das nicht
  mehr stimmt, ist die Trennung eine Täuschung.
