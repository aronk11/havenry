# ADR-0033 — Updates auf Klick, samt der schlechten Nachrichten

**Status:** Akzeptiert · **Datum:** 2026-08-21

## Kontext
Der Ablauf, den heute jeder von Hand macht: `docker compose pull`, `up -d`,
hoffen. Wenn etwas bricht, sucht man in fremden Release Notes nach dem Grund —
falls man überhaupt merkt, dass es an dem Update lag.

Watchtower automatisiert das Ziehen und lässt genau den Teil weg, der wehtut:
Man erfährt vorher nichts und hinterher nur, dass etwas nicht mehr läuft.

## Die eigentliche Einsicht
**Das Update ist nicht das Problem. Die fehlende Vorwarnung ist es.**

Wer ein Update verschiebt, tut das nicht aus Faulheit, sondern weil er nicht
weiß, was er sich einhandelt. Ein Werkzeug, das nur schneller updatet, löst das
falsche Problem — es beschleunigt das Risiko.

Deshalb ist der Mehrwert nicht „ein Klick", sondern:

> Vor dem Klick steht, was sich ändert. Nach dem Klick gibt es einen Weg zurück.

## Entscheidung

**1. Verfügbare Updates werden gesammelt angezeigt**, nicht als Dauerrauschen.
Je Dienst: laufender Digest, verfügbarer Digest, Versionssprung.

**2. Der Versionssprung wird eingeordnet.** Aus Tags wie `10.9.2 → 10.10.0`
liest Havenry, ob es sich um Patch, Nebenversion oder Hauptversion handelt.
Eine Hauptversion wird als solche gekennzeichnet — sie ist der Fall, bei dem
man vorher lesen sollte.

**3. Release Notes werden dazugeholt, wo es geht.** Trägt ein Image ein
`org.opencontainers.image.source`-Label (die meisten gepflegten tun das),
kennt Havenry das Repository und kann die Release Notes zum Zieltag abrufen und
anzeigen — samt Hervorhebung von Zeilen, die nach brechenden Änderungen
aussehen.

**Ehrlich zur Grenze:** Das ist eine Textsuche in fremden Release Notes, keine
Garantie. Sie findet „BREAKING", „⚠️", „Migration required" und Ähnliches. Was
dort nicht steht, findet sie nicht. Die Anzeige sagt das auch — sie behauptet
nie „keine brechenden Änderungen", sondern höchstens „nichts gefunden".

**4. Vor dem Update wird gefragt, nicht danach informiert.** Bei einer
Hauptversion oder gefundenen Hinweisen muss der Nutzer bestätigen, dass er es
gelesen hat. Bei einem Patch genügt der Klick.

**5. Ein Weg zurück existiert immer** — Digest-Pinning und Health-Fenster gab
es schon (ADR-0007). Neu ist, dass die Oberfläche ihn vorher benennt, nicht
erst im Fehlerfall.

**6. Automatisch nur, was der Nutzer erlaubt.** Der Modus `updates` je Stack
(ADR-0014) bleibt: `notify` als Vorgabe, `auto` opt-in — und `auto` gilt
**niemals für Hauptversionen**. Ein Werkzeug, das nachts eine Hauptversion
einspielt, ist genau das, wovor Leute Angst haben.

## Alternativen
- **Wie Watchtower nur ziehen:** löst das falsche Problem.
- **Eigene Datenbank gepflegter Änderungshinweise:** wäre besser und ist
  unrealistisch. Sie müsste für tausende Images gepflegt werden; das ist ein
  eigenes Produkt und veraltet ab dem ersten Tag.
- **Nur bei Hauptversionen warnen:** verpasst die Fälle, in denen jemand eine
  brechende Änderung in eine Nebenversion legt. Das kommt vor.

## Konsequenzen
- Havenry ruft für Release Notes eine fremde Seite auf. Das ist ein
  Netzwerkaufruf nach außen — er passiert **nur auf Anforderung**, nie im
  Hintergrund, und ist abschaltbar. Bei einem Werkzeug, das mit „kein
  Phone-Home" wirbt (ADR-0018), wäre alles andere ein Wortbruch.
- Ohne Quell-Label gibt es keine Release Notes. Dann wird angezeigt, was
  bekannt ist: der Versionssprung. Keine Erfindung.
- Ratenbegrenzung der Gegenstelle ist einzuplanen; Antworten werden
  zwischengespeichert.
