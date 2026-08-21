# ADR-0026 — Was verglichen wird und was bewusst nicht

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Der Drift-Vergleich ist das Produktmerkmal. Er ist zugleich die Stelle mit dem
größten Schadenspotenzial: Ein Falsch-positiv — eine gemeldete Abweichung, wo
keine ist — zerstört das Vertrauen in *jede* weitere Meldung. Der Nutzer lernt,
die Anzeige zu ignorieren, und dann ist das Werkzeug wertlos.

Ein übersehener Drift ist ärgerlich. Ein Falsch-positiv ist schlimmer.

## Entscheidung
Verglichen wird **nur, was auf beiden Seiten zuverlässig bekannt ist.**

**Verglichen:**
- Image, nach Normalisierung (`nginx` = `docker.io/library/nginx:latest`)
- Veröffentlichte Ports, sortiert, mit Protokoll
- Vom Nutzer gesetzte Labels — nur die, die in der Compose-Datei stehen
- Vorhandensein: im Repo beschrieben aber nicht da / da aber nicht im Repo
- Laufzustand, als eigene Kategorie `stopped`

**Bewusst nicht verglichen:**
- **Umgebungsvariablen.** Am Container stehen alle, auch die aus dem Image und
  die von Compose ergänzten. Ein Vergleich würde dauerhaft Rauschen erzeugen.
  Zudem stünden Werte im Klartext in der Oberfläche (ADR-0006).
- **Volumes.** Compose erzeugt Namen und Pfade, die sich nicht verlässlich auf
  die Angabe in der Datei zurückführen lassen.
- **Netzwerke.** Gleiches Problem; Compose legt eigene an.
- **Container-Ports ohne Host-Anteil.** Sie bekommen einen zufälligen Host-Port
  und würden bei jedem Neustart als Abweichung erscheinen.
- **Neustart-Regel**, solange der Agent sie nicht meldet — ein unbekannter
  Ist-Wert wird nicht verglichen, statt geraten.
- **Lokal gebaute Images** (`build:`). Sie tragen keinen vergleichbaren
  Bezeichner. Der Dienst erscheint mit Warnung, das Image wird übersprungen.
- **Zusätzliche Labels am Container.** Basis-Images bringen eigene mit; die
  Liste der von Werkzeugen gesetzten ist nicht abschließend bekannt.

**Nicht prüfbar ist nicht dasselbe wie in Ordnung.** Ist der Host getrennt oder
die Compose-Datei unlesbar, wird das als eigener Zustand angezeigt — niemals
als „stimmt überein" und niemals als Liste fehlender Dienste.

## Konsequenzen
- Der Vergleich ist bewusst unvollständig. Das ist die richtige Wahl: Ein
  Werkzeug, dem man glaubt, wenn es etwas meldet, ist mehr wert als eines, das
  alles meldet.
- Jede Erweiterung der Liste braucht zuerst einen Test, der belegt, dass die
  Normalisierung beider Seiten übereinstimmt.
- Was nicht verglichen wird, gehört in die Dokumentation. Eine stille Lücke
  wäre schlimmer als eine benannte.
