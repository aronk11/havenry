# havenryctl

**Die CLI liegt in einem eigenen Repository:**
<https://github.com/aronk11/havenry-cli>

Sie steht unter Apache-2.0, während der Kern AGPL-3.0 ist. Der Grund steht in
[ADR-0032](adr/0032-repo-aufteilung.md): Eine CLI wird eingebunden — in
Skripte, in CI-Pipelines, in fremde Werkzeuge. Unter AGPL müsste jeder, der den
API-Client übernimmt, sein eigenes Programm offenlegen.

```bash
go install github.com/aronk11/havenry-cli/cmd/havenryctl@latest
```

## Einrichten

```bash
export HAVENRY_SERVER=https://havenry.fritz.box:8443
export HAVENRY_TOKEN=<in der Oberfläche unter "Mein Konto" erzeugen>
export HAVENRY_INSECURE=1   # bei selbstsigniertem Zertifikat (ADR-0024)
```

## Befehle

| Befehl | Was er tut |
|---|---|
| `hosts` | Hosts mit Zustand, Container-Zahl und Agent-Version |
| `containers` | Container über alle sichtbaren Hosts |
| `drift` | Abweichungen zwischen Git und den Hosts |
| `logs <host> <container>` | Log-Stream folgen |
| `start\|stop\|restart <host> <container>` | Container steuern |
| `revert <host> <stack>` | Stack auf den Stand des Repos bringen |
| `teams`, `users`, `events` | Verwaltungsübersichten |
| `whoami` | Eigene **wirksame** Rechte samt Herkunft |
| `version` | Version von CLI und Server, unterstützte API-Versionen |

## Zwei Ausgabeformen

Ohne Optionen für Menschen, mit `--json` für Skripte. Beides stammt aus
derselben Antwort — die Tabelle kann also nicht irgendwann etwas anderes
behaupten als das JSON.

```bash
havenryctl --json drift | jq '.drift[] | select(.in_sync == false) | .stack'
```

`drift` liefert **Exitcode 0**, auch wenn Abweichungen bestehen: Ein Befund ist
kein Fehler des Aufrufs. Wer in einem Skript darauf reagieren will, wertet
`--json` aus.

## whoami zeigt die wirksame Rolle

Seit Teams (ADR-0029) können Rechte aus mehreren Quellen kommen. `whoami` zeigt
deshalb, was tatsächlich gilt — und woher:

```
dev  (operator)
hosts:  alle
        (am Konto: viewer — angehoben über direct, team:ops)
teams:  ops
rechte: hosts.view, containers.control, containers.logs, drift.resolve
```

Ohne diesen Hinweis wirkt die Anzeige wie ein Fehler: Am Konto steht `viewer`,
in der Oberfläche erscheinen Operator-Knöpfe.

## Versionsprüfung

Beim Start fragt die CLI `/api/versions` und warnt auf **stderr**, wenn der
Server ihre API-Version als veraltet meldet oder gar nicht mehr kennt. Sie
weigert sich deswegen nicht zu arbeiten — eine CLI, die stehenbleibt, weil eine
Auskunft nicht erreichbar war, ist schlimmer als eine, die es versucht.

Die Warnung geht auf stderr, damit `--json` pipebar bleibt.
