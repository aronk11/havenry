# Code-Qualität

Was hier steht, wurde gemessen, nicht geschätzt.

## Testabdeckung

| Paket | Abdeckung | Anmerkung |
|---|---|---|
| `internal/auth` | 87,5 % | Rechteauflösung — die heikelste Logik |
| `internal/reconcile` | 85,7 % | Drift-Vergleich |
| `internal/gitsync` | 73,5 % | gegen echte lokale Repos |
| `internal/store` | 64,7 % | vorher 38,1 % |
| `internal/provider/docker` | 54,1 % | gegen einen selbstgebauten Daemon |
| `internal/controlplane` | 42,6 % | Rechte auf HTTP-Ebene geprüft |
| `internal/agent` | 12,9 % | Log-Lebenszyklus; der Rest über Integration |
| `internal/transport` | 3,9 % | Protokollformat direkt, Verbindungslogik über Integration |

**Zwei Zahlen brauchen Erklärung, sonst wirken sie besser oder schlechter als
sie sind.**

`internal/transport` misst niedrig, weil Hub und Client fast ausschließlich
über Integrationstests in `internal/controlplane` laufen — Enrollment,
Reconnect, Liveness, Log-Abbestellung. Diese Prüfungen zählen für ein anderes
Paket. Die Alternative wäre, dieselben Abläufe ein zweites Mal mit Attrappen
nachzubauen; das erhöht die Zahl und prüft weniger.

`internal/agent` misst niedrig aus demselben Grund. Was hier eigene Tests hat,
ist der Fall, den Integration nicht zeigt: dass ein Log-Stream samt offener
Docker-Verbindung wirklich endet.

## Was beim Refactoring gefunden wurde

**Die Persistenz von Nutzern, Sitzungen, Token und Teams hatte null
Abdeckung** — und zwar aus einem strukturellen Grund: Neben der SQLite-
Implementierung stand eine zweite, in-memory, nur für Tests. Sie konnte die
Nutzer- und Team-Schnittstellen gar nicht erfüllen, also lief der
Konformitätstest gegen eine Attrappe, die diese Methoden nicht kannte.

Zwei Implementierungen, von Hand synchron gehalten, sind ohnehin eine bekannte
Fehlerquelle. Die In-Memory-Variante ist entfernt; alle Tests laufen gegen
SQLite in einem temporären Verzeichnis. **Eine Implementierung kann nicht von
sich selbst abweichen.** Preis: ein paar Millisekunden pro Test.

Die neuen Tests prüfen nicht „speichert es", sondern das, wo Fehler wirklich
sitzen: greift `UNIQUE`, räumt `CASCADE` auf, ist die Namenssuche
schreibungsunabhängig, überlebt eine leere Host-Liste den Weg durch die
Kodierung, erzeugt zweimaliges Speichern des Repos eine zweite Zeile.

**Toter Code entfernt:** `HasRoleFrom` (nur vom eigenen Test benutzt),
`CommitAndPush` und `AbsPath` (durch `EditAndPush` bzw. die interne Variante
ersetzt, aber stehengeblieben).

## Aufgeteilte Dateien

`internal/agent/agent.go` von 581 auf 316 Zeilen — herausgezogen wurden
Credential-Verwaltung, Log-Streaming und Kommandoausführung. Jede Datei trägt
jetzt eine Verantwortung und einen Kopfkommentar, der sie benennt.

`internal/controlplane/server.go` von 467 auf 167 Zeilen — Host-, Container-
und Log-Handler liegen in eigenen Dateien. Was bleibt, ist Aufbau, Router und
Lebenszyklus.

Über 400 Zeilen liegen noch `handlers_auth.go`, `cmd/havenryctl/commands.go`,
`transport/hub.go` und `handlers_repo.go`. Das sind bewusst zusammenhängende
Einheiten; sie zu teilen würde Zusammengehöriges auseinanderreißen, ohne etwas
verständlicher zu machen.

## Mutationstests

Jeder neue Testsatz wurde durch absichtlich eingebaute Fehler geprüft. Ein
Test, der beim ersten Lauf besteht, ist unbewiesen.

| Bereich | Mutationen | gefangen |
|---|---|---|
| Rechteauflösung | 3 | 3 |
| Persistenz | 5 | 5 |
| Protokollformat | 5 | 5 |

Geprüft wurden unter anderem: `COLLATE NOCASE` entfernt, `ON DELETE CASCADE`
entfernt, `PRAGMA foreign_keys` abgeschaltet, Ablaufprüfung der Sitzung
entfernt, schwächste statt stärkster Rolle, `ErrProtocolMismatch` nicht mehr
fatal, `compose_yaml` nicht übertragen, `omitempty` beim Credential entfernt.

Bei einem früheren Durchgang hatte genau dieses Vorgehen zwei Tests entlarvt,
die aus dem falschen Grund bestanden — deshalb gehört es zum Vorgehen und
nicht zur Kür.

## Werkzeuge

`gofmt` und `go vet` sind sauber. `.golangci.yml` ist bewusst schlank
gehalten: Ein Linter, den man ständig unterdrückt, erzieht dazu, Warnungen zu
ignorieren.
