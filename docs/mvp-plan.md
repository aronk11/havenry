# MVP-Arbeitsplan (v0.1)

## Definition of Done für v0.1

> Ein Nutzer installiert Control Plane und zwei Agenten in unter 10 Minuten, verbindet ein
> Git-Repo mit drei Compose-Stacks, sieht alle Container an einem Ort, erkennt eine manuell
> gemachte Änderung als Drift, entscheidet sich für adopt oder revert, und aktualisiert
> einen Service mit funktionierendem Rollback.

Alles, was dafür nicht nötig ist, ist nicht v0.1.

---

## Meilensteine

### M0 — Fundament
Repo, Lizenz (ADR-0010 entscheiden), CI, Lint, Release-Pipeline für amd64/arm64,
Projektname. Leeres Agent- und Control-Plane-Binary, die sich bauen und starten lassen.

**Fertig, wenn:** ein Tag ein Release mit Binaries und Container-Images erzeugt.

### M1 — Verbindung
WebSocket-Transport, Nachrichtenschema v1, Enrollment-Flow (ADR-0015), Heartbeat,
Reconnect mit Backoff, Host-Registry in der DB.

**Fertig, wenn:** ein Agent auf einem zweiten Rechner sich enrollt, in der UI erscheint
und einen Netzwerkausfall von 5 Minuten ohne manuellen Eingriff übersteht.

**Risiko:** höchstes technisches Risiko des MVP. Zuerst bauen, nicht zuletzt.

### M2 — Sichtbarkeit (Pain P1)
Docker-Provider: Container, Images, Ressourcen, Health auslesen. Report-Schleife.
Host- und Container-Übersicht in der UI. Log-Streaming. Start/Stop/Restart.

**Fertig, wenn:** kein SSH mehr nötig ist, um zu sehen, was auf allen Hosts läuft.
Das ist der erste Moment, in dem das Tool für dich selbst nützlich wird — ab hier
im eigenen Homelab produktiv nutzen.

### M3 — Git-Anbindung (Pain P2, Teil 1)
Repo verbinden (HTTPS/SSH-Deploy-Key), Klonen, Polling plus optionaler Webhook,
Stack-Erkennung nach Konvention (ADR-0014), optionale `stack.yaml`.

**Fertig, wenn:** die Stacks aus dem Repo den richtigen Hosts zugeordnet in der UI stehen.

### M4 — Drift (Pain P2, Teil 2) — das Kernstück
Soll-Zustand aus Compose normalisieren, Ist-Zustand normalisieren, **semantisch** diffen.
Diff-Ansicht in der UI. `adopt` (Commit ins Repo) und `revert` (Ist auf Git-Stand ziehen).
Modus `observe`/`apply` pro Stack.

**Fertig, wenn:** eine manuell auf dem Host geänderte Portfreigabe als Drift erscheint,
verständlich dargestellt ist, und beide Wege funktionieren.

**Risiko:** die Normalisierung ist unterschätzt aufwändig — Compose-Defaults, implizite
Werte, Image-Tag vs. Digest, Netzwerk- und Volume-Namensgebung. Hier entsteht die
Qualität des Produkts. Falsch-positive Drifts zerstören das Vertrauen sofort.

### M5 — Updates & Rollback (Pain P3)
Digest-Auflösung gegen Registry mit Cache, Update-Verfügbarkeit anzeigen, Update mit
Health-Fenster, Rollback auf vorherigen Digest, Digest-Historie.

**Fertig, wenn:** ein absichtlich kaputtes Image automatisch zurückgerollt wird.

### M6 — Politur & Release
Onboarding-Flow, Fehlermeldungen, README mit ehrlichen Grenzen (Docker-Socket-Risiko,
Rollback-Grenzen bei Datenmigration), Installationsanleitung, Screenshots, Demo.

**Fertig, wenn:** jemand, der das Projekt nicht kennt, es ohne Rückfragen zum Laufen bringt.

---

## Reihenfolge-Logik

M1 zuerst, weil es das größte technische Risiko trägt — scheitert der Transport hinter
NAT zuverlässig, ändert das die gesamte Architektur. M2 danach, weil es der früheste
Punkt ist, an dem du das Tool selbst täglich nutzt; ab da wird das Produkt vom eigenen
Gebrauch getrieben statt von Annahmen. M4 ist das Herzstück und braucht die meiste
Sorgfalt.

## Bewusst nicht im MVP

Backups, Netzwerk-Integrationen (Cloudflare/Tailscale), Proxmox, Secrets-Verwaltung,
Multi-User/RBAC, Benachrichtigungen, App-Katalog/One-Click-Installs, mobile Apps,
Metrik-Historie über 24 h hinaus, Agent-Selbstupdate.

Jedes dieser Themen hat eine ADR oder einen Roadmap-Eintrag — keins verschwindet,
alle warten auf echtes Nutzerfeedback.

---

## Roadmap nach v0.1

**Stand:** M0 bis M6 sind umgesetzt, dazu vorgezogen: Authentifizierung mit Rollen und
Host-Beschränkung, TLS als Vorgabe, Ratenlimit, sowie `revert`/`adopt`/`apply` zur
Auflösung von Abweichungen. Siehe [Stand](status.md).

| Version | Inhalt |
|---------|--------|
| v0.2 | Backup-Status zentral, Restore-Verifikation, Notifier (ntfy/Webhook/E-Mail), Agent-Selbstupdate |
| v0.3 | Cloudflare Tunnel / Tailscale geführt, SOPS-Unterstützung, Proxmox read-only |
| v0.4 | Native Apps (SwiftUI, Kotlin Compose), Push, Widgets |
| v0.5+ | Multi-User, öffentliche Provider-Schnittstelle, ggf. gehostete Control Plane |

## Go/No-Go nach v0.1

Vor dem Bau von v0.2 ehrlich prüfen:
- Nutzt du es selbst täglich? Wenn nein — der wichtigste Warnindikator.
- Gibt es Nutzer außerhalb deines Umfelds, die es installiert **behalten** haben?
- Kommen Issues, die auf echten Gebrauch hindeuten (nicht nur Installationsprobleme)?

Wenn nach drei Monaten keins davon zutrifft: Positionierung überarbeiten oder ehrlich
einstellen, statt Features nachzuschieben.
