# Getroffene Entscheidungen

Alle vier offenen Punkte sind entschieden und umgesetzt. Jede Entscheidung ist
umkehrbar — mit unterschiedlichem Aufwand, der hier jeweils dabeisteht.

## 1. Name: Havenry

*Haven* — ein sicherer Ort für deine Dienste — mit der Endung von *Foundry*.
Zwei Silben, auf Anhieb richtig gelesen, klingt nach Plattform statt nach
Skript.

Geprüft:

- **GitHub:** kein einziges Repository trägt diesen Namen. Auf einer Plattform
  mit hunderten Millionen Repos ist das die Ausnahme — du bist eindeutig
  auffindbar.
- **Domains:** `havenry.dev` und `havenry.io` hatten zum Prüfzeitpunkt keine
  DNS-Einträge. Das ist ein starkes, aber kein endgültiges Indiz: Registrierte
  Domains ohne Nameserver gibt es. **Vor dem Kauf beim Registrar gegenprüfen.**
- **Marken:** keine Kollision im Softwarebereich gefunden. Das war kein
  anwaltlicher Vorgang, sondern eine Websuche — für ein Hobbyprojekt
  angemessen, bei Kommerzialisierung ist eine echte Recherche das Geld wert.

**Verworfen wurde `Composure`**, obwohl das Wortspiel mit Docker *Compose*
reizvoll war: Ein Name, der bewusst nach dem Produktnamen eines anderen klingt,
in exakt derselben Kategorie, ist genau die Konstellation, in der
Verwechslungsgefahr geprüft wird. Docker Inc. hat Namensrechte in der
Vergangenheit durchgesetzt.

**Umkehrbar:** ja, mechanisch. Modulpfad, Repo, Registry-Namensraum und
`SourceURL`. Solange niemand von außen verlinkt, kostet es einen
Suchen-Ersetzen-Lauf.

## 2. Lizenz: AGPL-3.0, ohne CLA

Der Schutz gegen gehostete Kopien Dritter ist real und kostet hier wenig: Die
Zielgruppe sind Einzelpersonen und kleine Teams. Firmen, deren Richtlinien AGPL
ausschließen, gehören ohnehin nicht dazu — ADR-0001 schließt das
Enterprise-Segment bewusst aus.

**Kein CLA** ist die bewusste Kehrseite: Nach dem ersten fremden Beitrag ist eine
Umlizenzierung nur mit Zustimmung aller Beitragenden möglich. Dafür ist die
Hürde für Beiträge null — und ein Projekt, das von Community-Wachstum lebt,
braucht das mehr als die theoretische Option auf einen Lizenzwechsel.

**Umkehrbar:** bis zum ersten fremden Commit vollständig. Danach nicht mehr
allein. Das ist der einzige Punkt mit einem echten Stichtag.

## 3. Kommerzialisierung: beide Türen bleiben offen

Kein Entweder-oder nötig. Die AGPL schließt ein späteres kommerzielles Angebot
nicht aus — sie schützt es: Als Rechteinhaber darfst du dein eigenes Werk
zusätzlich unter anderen Bedingungen anbieten. Dritte, die eine gehostete
Fassung verkaufen wollen, müssen ihre Änderungen offenlegen.

Es wurde also nichts verbaut. Die Kandidaten bleiben eine gehostete Control
Plane (löst das Erreichbarkeitsproblem aus ADR-0003), Push für die mobilen Apps
und Team-Funktionen.

**Umkehrbar:** die Frage stellt sich erst, wenn du sie stellen willst.

## 4. SQLite-Treiber: als Skript hinterlegt

`scripts/switch-to-pure-go-sqlite.sh` — einmalig mit Netzzugang ausführen.
Wechselt auf `modernc.org/sqlite`, stellt Makefile und Dockerfile auf CGO-frei,
räumt auf, baut und testet.

Ich konnte es hier nicht ausführen, weil `modernc.org` nicht erreichbar war.
Danach sind beide Binaries CGO-frei.

---

## Was mit der Lizenz zusätzlich fällig war

Die AGPL verlangt in §13, dass Nutzer, die die Software **über ein Netz**
benutzen, an den Quelltext der laufenden Fassung kommen. Das ist bei einer
Weboberfläche keine Formalie, sondern der Kern dieser Lizenz.

Umgesetzt: Endpunkt `/source` und ein Verweis in Kopfzeile und
Anmeldebildschirm. **Wer das Projekt abwandelt, muss `SourceURL` in
`internal/controlplane/server.go` auf den eigenen Quelltext ändern** — sonst
verweist die eigene Fassung auf fremden Code und die Lizenzbedingung ist nicht
erfüllt.

## Was weiterhin bewusst fehlt

Unverändert und begründet: Netzwerkzugang von unterwegs (ADR-0023),
SSH-Zugriff durch die Plattform (ADR-0023), `adopt` für Ports und Labels
(ADR-0028), Vergleich von Umgebungsvariablen und Volumes (ADR-0026). Backups,
Proxmox und die mobilen Apps stehen auf der Roadmap v0.2–v0.4.
