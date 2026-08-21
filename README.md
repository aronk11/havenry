# Havenry

**Ein ruhiger Ort für deine Dienste.**

Havenry verwaltet verteilte Docker-Hosts im Homelab: ein Dashboard für alle
Hosts, Git als Quelle der Wahrheit, Abweichungen sichtbar statt still, Updates
mit Rollback.

Eine leichtgewichtige Management-Plattform für verteilte Docker-Hosts im Homelab.
Ein Dashboard für alle Hosts, Git als Source of Truth, Drift sichtbar statt still,
Updates mit Rollback.

## Warum noch ein Tool?

Portainer zeigt dir Container, aber nicht, was von deiner Git-Konfiguration abweicht.
Klassische GitOps-Tools gleichen automatisch an — und räumen nachts dein Experiment weg.
Watchtower updatet, aber kennt keinen Weg zurück.

Diese Plattform macht drei Dinge anders:

- **Deine Compose-Files bleiben deine.** Kein eigenes Format, kein Lock-in. Deinstallier
  das Tool und dein Setup läuft weiter. (ADR-0002)
- **Drift wird gezeigt, nicht überschrieben.** Default ist `observe`: du siehst, was sich
  geändert hat, und entscheidest — ins Repo übernehmen oder zurücksetzen. Automatik ist
  opt-in pro Stack. (ADR-0004)
- **Updates mit echtem Rollback.** Digest-Pinning und Health-Fenster statt Hoffnung. (ADR-0007)

## Status

**Funktionsfähig.** Hosts verbinden, Container sehen und steuern, Logs lesen,
Git-Repo anbinden, Abweichungen erkennen und auflösen, Nutzer verwalten.
Siehe [Stand](docs/status.md) und [ADRs](docs/adr/).

Siehe [getroffene Entscheidungen](docs/ENTSCHEIDUNGEN.md).

## Loslegen

```bash
# 1. Control Plane starten
docker compose -f deploy/compose.yaml up -d

# 2. Startpasswort holen — es steht genau einmal im Protokoll
docker logs havenry | grep Passwort

# 3. Oberfläche öffnen und anmelden
#    https://<host>:8443
#    Beim ersten Aufruf warnt der Browser: Das Zertifikat ist selbstsigniert.
#    Den Fingerprint aus dem Protokoll vergleichen, dann fortfahren.
#    docker logs havenry | grep fingerprint

# 4. In der Oberfläche "Host hinzufügen" — das Token gilt 15 Minuten.
#    Dann auf jedem Host, den Havenry sehen soll:
ENROLL_TOKEN=<token> \
HAVENRY_SERVER=wss://<host>:8443/agent \
  docker compose -f deploy/agent.yaml up -d

# 5. Host in der Oberfläche bestätigen — vorher führt er keine Kommandos aus.
```

Die beiden Compose-Dateien sind absichtlich getrennt: Der Agent braucht ein
Token, und das gibt es erst, wenn die Control Plane läuft.

Für `revert` und den Modus `apply` braucht ein Host zusätzlich `docker compose`
(ADR-0027). Fehlt es, meldet der Agent das beim Start und die Oberfläche blendet
den Knopf aus, statt ihn scheitern zu lassen.

## Was es nicht ist

- Kein Kubernetes-Tool (ADR-0001)
- Kein PaaS — es deployt keine Apps für dich
- Kein Secrets-Manager (ADR-0006)
- Kein Ersatz für Prometheus/Grafana (ADR-0018)

## Zugriffsmodell

Drei Rollen (`admin`, `operator`, `viewer`), jeweils auf bestimmte Hosts beschränkbar.
Jede Aktion wird mit Nutzernamen protokolliert. API-Token erben die Rechte ihres
Kontos — nie mehr. Details in ADR-0022.

Zu „Zero Trust": Die Plattform übernimmt die Absicherung *zwischen* Control Plane und
Agenten sowie den rechtegeprüften Zugriff *durch* die Plattform. Den Netzwerkzugang
selbst übernimmt sie bewusst nicht — dafür gibt es Tailscale, WireGuard und Cloudflare
Tunnel. Siehe ADR-0023.

## Ehrliche Hinweise

- Der Agent braucht Zugriff auf den Docker-Socket. Das ist faktisch Root auf dem Host.
  Das ist ein reales Risiko und keine Formalie — der Agent läuft deshalb als root,
  statt es mit einer anderen Nutzer-ID zu verschleiern.
- Rollback über Image-Digests macht **keine** Datenmigrationen rückgängig, die ein
  Container beim Start durchgeführt hat.
- **Kein Phone-Home.** Keine Telemetrie, auch nicht anonym, auch nicht opt-out.
- **TLS ist an, sofern nicht abgeschaltet.** Beim ersten Start wird ein selbstsigniertes
  Zertifikat erzeugt; der Fingerprint steht im Protokoll und sollte im Browser
  verglichen werden.
- **Der Drift-Vergleich ist bewusst unvollständig** (ADR-0026): Umgebungsvariablen,
  Volumes und Netzwerke werden nicht verglichen, weil das dauerhaft Rauschen erzeugen
  würde. Ein Werkzeug, dem man glaubt, wenn es etwas meldet, ist mehr wert als eines,
  das alles meldet.
- **`git` muss auf dem Host der Control Plane installiert sein** (ADR-0021).

## Repository-Aufbau

```
cmd/            Einstiegspunkte: havenry (Control Plane), havenry-agent
internal/       Kern: transport, provider/docker, gitsync, reconcile, auth
internal/store/ Schnittstellen + Registry; Backends in sqlitestore/, memstore/
deploy/         Compose-Beispiele für Control Plane und weitere Hosts
web/            Marketing-Seite (React/Vite, statisch, eigenes UI-Kit)
docs/           Architektur, Stand, ADRs
scripts/        Hilfsskripte (u. a. SQLite-Treiberwechsel)
```

## Mitarbeiten

Einmal nach dem Klonen:

```bash
./scripts/install-hooks.sh
```

Das richtet den `commit-msg`-Hook (Conventional Commits) und die
Commit-Vorlage ein. Details in [CONTRIBUTING.md](CONTRIBUTING.md).

## Dokumentation

- [Architektur](docs/architecture.md)
- [MVP-Plan](docs/mvp-plan.md)
- [Kommandozeile](docs/cli.md) — eigenes Repository
- [Repo-Aufteilung](docs/adr/0032-repo-aufteilung.md)
- [Code-Qualität](docs/qualitaet.md)
- [Modularität](docs/modularitaet.md)
- [Architecture Decision Records](docs/adr/)

## Lizenz

[AGPL-3.0](LICENSE) — siehe ADR-0010.

Die AGPL verlangt in §13, dass Nutzer, die havenry über ein Netz benutzen, an
den Quelltext der laufenden Fassung kommen. Dafür gibt es den Endpunkt `/source`
und einen Verweis in der Oberfläche. **Wer havenry abwandelt und betreibt, muss
`SourceURL` in `internal/controlplane/server.go` auf den eigenen Quelltext
ändern.**
