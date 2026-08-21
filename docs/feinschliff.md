# Feinschliff — was dabei gefunden wurde

`go vet` war sauber, alle Tests grün. Trotzdem hat die Durchsicht sechs echte
Mängel zutage gefördert. Fünf davon hätte nie ein Test gefunden, weil es keine
gab — der sechste kam heraus, während ein neuer Test geschrieben wurde.

## Die Befunde

**A — Log-Streams liefen auf dem Agenten unbegrenzt weiter.**
Schloss der Browser die Log-Ansicht, erfuhr der Agent nichts davon. Bei
`follow=true` blieb eine Goroutine samt offener Docker-Verbindung dauerhaft
bestehen. Jeder Log-Aufruf hinterließ eine Leiche. Behoben durch eine
Abbestellung im Protokoll.

**B — Eine Neuverbindung löschte den frisch gemeldeten Zustand.**
Beim Verbindungswechsel räumt die alte Sitzung verzögert auf. Dieses Aufräumen
verwarf den Zustand des Hosts — auch dann, wenn längst eine neue Sitzung stand.
Die Oberfläche zeigte dann grundlos „noch keine Zustandsmeldung". Behoben durch
eine Prüfung, ob wirklich niemand mehr verbunden ist.

**C — `adopt` konnte von der Synchronisation überrollt werden.**
Zwischen dem Schreiben der Compose-Datei und dem Commit konnte der
60-Sekunden-Takt ein `reset --hard` ausführen. Die Änderung wäre spurlos
verschwunden, der Nutzer hätte einen Erfolg gesehen, der keiner war. Behoben:
Lesen, Ändern, Committen und Pushen laufen unter derselben Sperre.

**D — Die Schreibrechtsprüfung war ein Netzwerkaufruf bei jedem Abruf.**
Die Drift-Ansicht wird alle fünf Sekunden abgerufen. Das hieß: alle fünf
Sekunden ein `git push --dry-run` gegen die Gegenstelle, pro angemeldetem
Nutzer. Bei GitHub führt das zügig zu einer Drosselung. Behoben durch einen
Zwischenspeicher mit fünf Minuten Gültigkeit — der Prüfzeitpunkt wird jetzt
mitgeliefert, damit „adopt ist ausgegraut" nachvollziehbar bleibt.

**E — Globaler veränderlicher Zustand im Agent-Paket.**
Die CPU-Messung braucht die vorherige Messung zur Differenzbildung und legte
sie in einer Paketvariablen ab — ohne Sperre und nicht testbar. Jetzt ein Feld
mit eigener Sperre.

**F — Gefunden vom neuen Test zu A.**
Zwei Log-Abonnements mit derselben ID stapelten sich falsch: Das verzögerte
Aufräumen des ersten löschte den Eintrag des zweiten. **Derselbe Fehlertyp wie
B** — verzögertes Aufräumen, das nicht prüft, ob der Eintrag noch der eigene
ist. Dass dasselbe Muster an zwei unabhängigen Stellen auftrat, ist der
eigentliche Hinweis: Es lohnt sich, bei jedem `defer delete(...)` zu fragen,
wem der Eintrag inzwischen gehört.

## Was die Mutationstests über die Tests selbst verrieten

Die erste Fassung der Regressionstests bestand **alle** — und fing **keine**
der eingebauten Mutationen:

- Der Test zu B hatte die Aufräumlogik im Test nachgebaut statt den echten
  Server zu verwenden. Er prüfte sich selbst.
- Der Test zu D lief ohne konfiguriertes Repo und kam damit nie in den Code.
- Der Test zu A prüfte, ob die Abbestellung ankommt — nicht, ob sie wirkt.
- Ein Zeitmaß für D wäre wertlos gewesen: Zwanzig `push --dry-run` gegen ein
  lokales Repo dauern 81 Millisekunden.

Alle vier neu geschrieben: gegen den echten Server, mit echtem Repo, und mit
einem Zähler offener Docker-Verbindungen statt Karteieinträgen als Maß.

**Die Lehre:** Ein Test, der beim ersten Lauf besteht, ist unbewiesen. Erst der
absichtlich eingebaute Fehler zeigt, ob er etwas misst.

## Nebenbei behoben

- Der Abstand vor einem Kommentar am Zeilenende bleibt bei `adopt` jetzt
  erhalten. Die Ausrichtung mehrerer Kommentare untereinander ist Absicht des
  Autors, keine zufällige Formatierung.
