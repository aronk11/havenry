# ADR-0030 — API-Versionierung

**Status:** Akzeptiert · **Datum:** 2026-08-20

## Kontext
Ab jetzt sprechen mehrere Dinge dieselbe API: die Weboberfläche, die CLI und
später die mobilen Apps. Eine CLI wird auf Rechnern installiert und dort
jahrelang nicht aktualisiert. Eine Änderung, die die Oberfläche unbemerkt
mitmacht, weil sie mit dem Server ausgeliefert wird, bricht dann still eine
CLI, die jemand vor achtzehn Monaten heruntergeladen hat.

## Entscheidung

**Alle Endpunkte liegen unter `/api/v1/`.** Es gibt keine unversionierten
Endpunkte außer `/healthz` und `/api/versions`.

**Eine Version ist ein eigener Baum, keine Einstellung.** `v2` wird eines Tages
neben `v1` gemountet und beide laufen gleichzeitig. Deshalb wird die
Registrierung der Routen pro Version gekapselt (`registerV1`), statt Pfade
verstreut im Router zu erzeugen — ohne diese Trennung ist ein zweiter Baum
später nicht sauber möglich.

**`/api/versions`** meldet, welche Versionen dieser Server kennt und welche als
veraltet gelten. Ein Client kann damit beim Start prüfen, ob er noch bedient
wird, statt an einem 404 zu raten.

**Jede Antwort trägt `X-Havenry-API-Version`.** Damit sieht man in Protokollen
und beim Debuggen, welcher Baum geantwortet hat.

### Was als brechend gilt
Innerhalb von `v1` ist erlaubt: neue Endpunkte, neue **optionale** Felder in
Anfragen, neue Felder in Antworten. Nicht erlaubt: Felder entfernen oder
umbenennen, Typen ändern, Pflichtfelder ergänzen, Bedeutungen ändern,
Statuscodes für bestehende Fälle ändern.

**Ein Client darf unbekannte Felder in Antworten nicht als Fehler behandeln.**
Das steht in der API-Dokumentation, weil es sonst niemand tut — und dann ist
jedes neue Feld faktisch brechend.

### Rückzug einer Version
Wird `v2` eingeführt, bleibt `v1` mindestens zwölf Monate erhalten und wird in
`/api/versions` als veraltet gemeldet. Erst danach darf sie verschwinden.

## Was nicht mitversioniert wird
Das **Agent-Protokoll** hat seine eigene Version (ADR-0016) und ist an diesen
Zyklus nicht gebunden. Agent und Control Plane werden gemeinsam aktualisiert;
API-Clients nicht. Beides in einen Topf zu werfen hieße, den einen Zyklus dem
anderen aufzuzwingen.

## Konsequenzen
- Etwas mehr Gerüst als nötig, solange es nur `v1` gibt. Der Aufwand fällt an,
  wenn man ihn braucht — nachträglich einen zweiten Baum in einen gewachsenen
  Router einzuziehen ist deutlich teurer.
- Die CLI schickt ihre eigene Version im `User-Agent` und warnt, wenn der
  Server ihre API-Version als veraltet meldet.
