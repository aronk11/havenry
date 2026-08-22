# Architecture Decision Records

Jede ADR beschreibt **eine** Entscheidung mit Kontext, Alternativen und Konsequenzen.
Format: MADR-angelehnt, bewusst kurz.

**Status-Werte:** `Vorgeschlagen` · `Akzeptiert` · `Abgelehnt` · `Ersetzt durch ADR-XXXX`

| ADR | Titel | Status |
|-----|-------|--------|
| [0001](0001-docker-als-zielplattform.md) | Docker/Compose als Zielplattform, nicht Kubernetes | Akzeptiert |
| [0002](0002-git-als-source-of-truth.md) | Git ist Source of Truth, die DB ist abgeleitet | Akzeptiert |
| [0003](0003-agent-initiierte-verbindung.md) | Agent-initiierte Outbound-Verbindung statt Push | Akzeptiert |
| [0004](0004-observe-vor-apply.md) | Drift wird angezeigt, nicht automatisch überschrieben | Akzeptiert |
| [0005](0005-single-binary-embedded-db.md) | Ein Binary, eingebettete DB, keine externen Dienste | Akzeptiert |
| [0006](0006-secrets-out-of-scope.md) | Kein eigenes Secrets-Management im MVP | Akzeptiert |
| [0007](0007-rollback-via-digest-pinning.md) | Rollback über Image-Digest-Pinning | Akzeptiert |
| [0008](0008-provider-interface.md) | Provider-Interface statt Plugin-System | Akzeptiert |
| [0009](0009-api-first-mobile-spaeter.md) | API-first, native Apps erst nach Web-Validierung | Akzeptiert |
| [0010](0010-lizenz-und-oss-modell.md) | Open-Source-Core, Lizenzwahl: AGPL-3.0 | Akzeptiert |
| [0011](0011-proxmox-read-only.md) | Proxmox nur lesend, kein VM-Lifecycle | Akzeptiert |
| [0012](0012-kein-terraform-im-kern.md) | Kein Terraform/OpenTofu im Kern | Akzeptiert |
| [0013](0013-transport-websocket.md) | WebSocket als Agent-Transport | Akzeptiert |
| [0014](0014-repo-layout-und-stack-mapping.md) | Repo-Layout und Stack-zu-Host-Zuordnung | Akzeptiert |
| [0015](0015-auth-und-agent-enrollment.md) | Auth und Agent-Enrollment | Akzeptiert |
| [0016](0016-monorepo-und-release.md) | Monorepo, Versionierung und Release-Prozess | Akzeptiert |
| [0017](0017-web-ui-stack.md) | Web-UI-Stack und Einbettung ins Binary | Akzeptiert |
| [0018](0018-observability-des-tools-selbst.md) | Observability der Plattform selbst | Akzeptiert |
| [0019](0019-liveness-fenster.md) | Serverseitiges Liveness-Fenster für Agent-Sitzungen | Akzeptiert |
| [0020](0020-sqlite-treiber.md) | SQLite-Treiber: reines Go statt CGO | Akzeptiert (Umstellung offen) |
| [0021](0021-git-ueber-binary.md) | Git über das `git`-Binary statt einer Bibliothek | Akzeptiert |
| [0022](0022-auth-und-rollen.md) | Nutzer, Rollen und Host-Beschränkung | Akzeptiert |
| [0023](0023-zero-trust-abgrenzung.md) | Zero Trust: Abgrenzung des Versprechens | Akzeptiert |
| [0024](0024-tls-standardmaessig.md) | TLS ist die Vorgabe, nicht die Option | Akzeptiert |
| [0025](0025-anmelde-ratenlimit.md) | Ratenlimit bei der Anmeldung | Akzeptiert |
| [0026](0026-was-verglichen-wird.md) | Was verglichen wird und was bewusst nicht | Akzeptiert |
| [0027](0027-compose-ueber-cli.md) | Compose-Ausführung über die Docker-CLI | Akzeptiert |
| [0028](0028-adopt-grenzen.md) | Grenzen von `adopt` | Akzeptiert |
| [0029](0029-teams.md) | Teams über Rollen, nicht statt Rollen | Akzeptiert |
| [0030](0030-api-versionierung.md) | API-Versionierung | Akzeptiert |
| [0031](0031-austauschbare-datenbank.md) | Austauschbare Datenbank | Akzeptiert |
| [0032](0032-repo-aufteilung.md) | Aufteilung in Repositories und Lizenzschichten | Akzeptiert |
| [0033](0033-updates-und-breaking-changes.md) | Updates auf Klick, samt der schlechten Nachrichten | Akzeptiert |
| [0034](0034-lokale-stacks-und-git-schreibzugriff.md) | Lokale Stacks, Git-Schreibzugriff und direktes Container-Management | Akzeptiert |
