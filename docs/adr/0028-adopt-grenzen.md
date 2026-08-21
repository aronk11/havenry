# ADR-0028 — Grenzen von `adopt`

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
ADR-0004 sieht zwei Wege vor, eine Abweichung aufzulösen: `revert` bringt den
Host auf den Stand des Repos, `adopt` schreibt den Zustand des Hosts ins Repo.

`revert` ist mechanisch. `adopt` bedeutet: eine YAML-Datei ändern, die einem
Menschen gehört — mit seinen Kommentaren, seiner Formatierung, seiner
Reihenfolge. Ein Werkzeug, das eine Compose-Datei neu erzeugt und dabei
Kommentare verliert, wird zu Recht nie wieder benutzt.

## Entscheidung
`adopt` verändert die Datei **nur bei einer geänderten Image-Angabe**, und dabei
nur den betroffenen Skalarwert — kein Neuschreiben der Datei.

Für alle anderen Abweichungsarten zeigt die Oberfläche die nötige Änderung an,
führt sie aber nicht aus. Der Nutzer trägt sie selbst ein.

## Begründung
Der Image-Tag ist der mit Abstand häufigste Fall (jemand hat auf dem Host ein
Update gemacht) und zugleich der einzige, bei dem eine punktgenaue Ersetzung
sicher ist: ein Wert, eine Zeile, keine Struktur.

Ports, Labels und fehlende oder zusätzliche Dienste erfordern strukturelle
Eingriffe — dort ist der Unterschied zwischen „richtig geändert" und „Datei
beschädigt" nicht mehr zuverlässig zu ziehen. Ein halbes Feature, das man
versteht, ist besser als ein ganzes, dem man nicht trauen kann.

## Umsetzung
- Geändert wird ausschließlich die Zeile mit dem `image:`-Wert innerhalb des
  betroffenen Dienstes. Einrückung, Kommentare und Anführungszeichen der
  Umgebung bleiben unberührt.
- Danach wird geprüft, ob die Datei noch gültiges Compose ist und ob genau die
  erwartete Änderung entstanden ist. Schlägt das fehl, wird nichts geschrieben.
- Commit und Push erfolgen als ein Schritt. Scheitert der Push (fehlender
  Schreibzugriff), wird der Commit zurückgenommen, damit die Arbeitskopie ein
  reines Abbild des Repos bleibt (ADR-0002).
- Ohne Schreibzugriff aufs Repo bleibt `adopt` deaktiviert; die Oberfläche sagt
  das, statt es erst beim Klick scheitern zu lassen.

## Konsequenzen
- `adopt` deckt einen Teil der Fälle ab. Das wird in der Oberfläche benannt,
  nicht kaschiert.
- Eine spätere Erweiterung auf Ports braucht einen kommentar-erhaltenden
  YAML-Umgang und eine eigene ADR.
