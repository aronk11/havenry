# Stand

## Was funktioniert

| Bereich | Stand |
|---------|-------|
| Transport | WebSocket, agent-initiiert, Reconnect, Liveness-Fenster |
| Enrollment | Einmal-Token → Credential, menschliche Bestätigung, widerrufbar |
| Persistenz | SQLite mit Migrationen, atomarem Token-Verbrauch |
| Docker | Eigener Engine-API-Client, Container lesen und steuern, Log-Streaming |
| Metriken | CPU, Speicher, Platte, Uptime, Load — direkt aus /proc |
| Git | Klonen, Fetch, Reset, Clean; Stack-Erkennung nach Konvention |
| Lokale Stacks | Compose-Definitionen direkt in Havenry anlegen/bearbeiten, ohne Git (ADR-0034) |
| Drift | Semantischer Vergleich, vier Kategorien, keine Falsch-positiven |
| Auflösung | `revert` über Compose-CLI, `adopt` für Image-Angaben, Modus `apply` |
| Auth | Drei Rollen, Host-Beschränkung, API-Token, Argon2id, Ratenlimit |
| TLS | An per Vorgabe, selbstsigniertes Zertifikat mit Fingerprint |
| Oberfläche | Anmeldung, Hosts, Stacks, Abweichungen, Nutzer, Repository, Konto |
| Auslieferung | Dockerfiles für beide Binaries, Compose-Beispiele, CI; Release-Tag baut und veröffentlicht Binaries und ghcr.io-Images (amd64/arm64, Agent zusätzlich arm/v7) |

## Zahlen

- 28 ADRs
- ~11.000 Zeilen Go
- Alle Tests grün mit `-race`
- Agent-Binary 5 MB (Budget 20 MB, ADR-0005)

## Was die Mutationstests aufgedeckt haben

Die Tests wurden mehrfach durch gezielt eingebaute Fehler geprüft. Gefunden:

1. **Zwei Drift-Tests bestanden aus dem falschen Grund** — einer lieferte bereits
   sortierte Werte, der andere prüfte eine Regel, die schon anderswo greift.
2. **Eine Mutation blieb unbemerkt im Code**, weil ein `git checkout` zum
   Wiederherstellen wirkungslos war (die Datei war noch nicht eingecheckt). Gefunden hat
   sie der frisch reparierte Test.
3. **Beim Ergänzen der Neustart-Regel** wurde „nicht gemeldet" zu „keine Regel" — und
   erzeugte sofort Falsch-positive bei jedem Stack mit `restart:`. Die Unterscheidung
   ist jetzt explizit.
4. **Das Ereignisprotokoll trug „user"** statt des Nutzernamens und protokollierte die
   Host-Bestätigung doppelt.
5. **Das Liveness-Loch:** Ein stumm verschwundener Agent galt minutenlang als verbunden.

## Beim Feinschliff behoben

Sechs Mängel, die `go vet` nicht sieht — darunter zwei Speicherlecks und ein
Wettlauf, der `adopt` stillschweigend hätte verpuffen lassen. Siehe
[feinschliff.md](feinschliff.md).

## Kleinigkeiten, die bekannt sind

- Der Modus `apply` prüft alle zwei Minuten. Bei einem frischen Push kann es also bis zu
  zwei Minuten dauern, bis angeglichen wird — dazu kommt der Repo-Takt von 60 Sekunden.
- Container ganz ohne Compose-Zuordnung erscheinen in der Stack-Übersicht unter
  „(ohne stack)", aber nicht im Drift-Vergleich.

## Offene Entscheidungen

Siehe [ENTSCHEIDUNGEN.md](ENTSCHEIDUNGEN.md) — fünf Punkte, alle bei dir.
