# TLS, Ratenlimit und M4 (Drift-Erkennung) — Status

## TLS ist jetzt die Vorgabe (ADR-0024)

Vorher optional, mit Warnung. Das war falsch: Was übersprungen werden kann, wird
übersprungen und dann nie nachgeholt.

Ohne konfiguriertes Zertifikat erzeugt die Control Plane beim ersten Start ein
selbstsigniertes — mit `localhost`, den Loopback-Adressen, dem Hostnamen und allen
lokalen IPv4-Adressen im Zertifikat, damit der Browser nicht wegen eines fehlenden
Eintrags Alarm schlägt. Der SHA-256-Fingerprint steht bei jedem Start im Protokoll und
lässt sich mit der Browser-Anzeige vergleichen. Eigene Zertifikate bleiben möglich.
`--no-tls` schaltet ab, mit unübersehbarer Warnung.

Laufzeit zehn Jahre statt 90 Tage: Vertraut wird ohnehin per Fingerprint. Ein Ablauf
hieße nur, dass eine Installation eines Tages von selbst aufhört zu funktionieren — der
schlechteste Fehlermodus für etwas, das im Keller läuft.

## Ratenlimit (ADR-0025)

Fünf Fehlversuche in 15 Minuten → 15 Minuten Sperre, gezählt **getrennt nach
Benutzername und Quelladresse**. Nur nach Adresse hilft nicht gegen verteilte Versuche;
nur nach Benutzername erlaubt es, fremde Konten gezielt auszusperren.

## M4 — der semantische Vergleich

**Verglichen wird nur, was auf beiden Seiten zuverlässig bekannt ist** (ADR-0026):
Image (normalisiert), veröffentlichte Ports, vom Nutzer gesetzte Labels, Vorhandensein,
Laufzustand. Umgebungsvariablen, Volumes und Netzwerke bewusst nicht — sie würden
dauerhaft Rauschen erzeugen.

Vier Abweichungsarten: `changed`, `missing`, `extra`, `stopped`. Ein gestoppter Dienst
ist keine Konfigurationsabweichung und bekommt deshalb eine eigene Kategorie.

**„Nicht prüfbar" ist ein eigener Zustand.** Ist ein Host getrennt, wird das angezeigt —
nicht als Liste fehlender Dienste. Sonst stünde die Oberfläche bei jedem kurzen Ausfall
voller Falschmeldungen.

## Die Mutationstests — und was sie aufgedeckt haben

Die Drift-Tests bestanden alle beim ersten Lauf. Das war verdächtig, also habe ich vier
Fehler gezielt eingebaut und geprüft, ob die Tests sie fangen:

| Mutation | Gefangen? |
|----------|-----------|
| Registry-Normalisierung entfernt | ja |
| `:latest` nicht ergänzt | ja |
| Label-Filter abgeschaltet | **nein** |
| Port-Sortierung entfernt | **nein** |

**Zwei Tests bestanden aus dem falschen Grund.**

`TestPortOrderDoesNotMatter` lieferte den Ist-Zustand bereits sortiert — der Test hätte
auch ohne jede Sortierung bestanden. Behoben: Die Ports kommen jetzt in umgekehrter
Reihenfolge herein.

`TestGeneratedLabelsAreIgnored` war durch eine ganz andere Regel geschützt (zusätzliche
Container-Labels werden nie gemeldet) und übte den Filter gar nicht aus. Ergänzt um
einen Test, der ein erzeugtes Label in der *Compose-Datei* prüft — der Fall, für den der
Filter da ist.

Beim Zurücknehmen der Mutationen kam noch etwas heraus: Ein `git checkout` zum
Wiederherstellen war wirkungslos, weil die Datei noch nicht eingecheckt war — die
Mutation blieb unbemerkt im Code. Gefunden hat sie der neu reparierte Test. Genau dafür
sind sie da.

## Nachgewiesen (E2E über HTTPS)

- Selbstsigniertes Zertifikat wird erzeugt, Fingerprint protokolliert, Klartext abgewiesen
- Sechster Anmeldeversuch → HTTP 429 mit verbleibender Wartezeit
- Drift erkannt: `radarr` im Repo aber nicht auf dem Host (`missing`),
  `caddy` vorhanden aber gestoppt (`stopped`)
- **Keine Falsch-positiven** für `jellyfin` und `sonarr`, obwohl der Ist-Zustand
  voll qualifizierte Images und Compose-Labels meldet

## Bekannte Grenzen

- **`adopt` und `revert` fehlen noch.** Abweichungen werden erkannt und angezeigt, aber
  noch nicht aufgelöst. Beides braucht Neues: `revert` erfordert Compose-Ausführung auf
  dem Agenten, `adopt` einen Schreibzugriff aufs Repo. Das ist der nächste Schritt.
- **Modus `apply` wird gelesen, aber nicht ausgeführt** — er setzt dieselbe
  Compose-Ausführung voraus.
- **Container ganz ohne Stack-Zuordnung** (von Hand gestartet, kein Compose) erscheinen
  in der Stack-Übersicht unter „(ohne stack)", aber nicht im Drift-Vergleich.
- **Der Agent meldet die Neustart-Regel nicht**, deshalb wird sie nicht verglichen.
- CGO für die Control Plane (ADR-0020), kein Webhook für sofortige Synchronisation.
