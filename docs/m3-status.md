# M3 + Auth — Status: abgeschlossen

## Ziel
M3 laut MVP-Plan: „Die Stacks aus dem Repo stehen den richtigen Hosts zugeordnet in der
Oberfläche." Zusätzlich vorgezogen: die Authentifizierung, die nach M2 als offene
Flanke benannt war.

## Umgesetzt

| Bereich | Ergebnis |
|---------|----------|
| Git-Anbindung | Klonen, Fetch, Reset, Clean über das `git`-Binary (ADR-0021), Takt 60 s |
| Stack-Erkennung | Konvention `stacks/<host>/<stack>/compose.yaml`, optionale `stack.yaml` (ADR-0014) |
| Probleme | Fehlerhafte Verzeichnisse werden gesammelt und angezeigt, nicht geworfen |
| Nutzer | Drei Rollen, Host-Beschränkung, Argon2id, Sitzungen (ADR-0022) |
| API-Token | An einen Nutzer gebunden, erbt dessen Rechte, eigene Ablaufzeit |
| Erster Start | Zufälliges Admin-Passwort einmalig im Protokoll, Änderungszwang |
| Oberfläche | Anmeldung, Reiter für Übersicht, Repository, Nutzer, eigenes Konto |
| Zero Trust | Abgegrenzt und dokumentiert (ADR-0023) |

## Zero Trust — was übernommen wird

Der Begriff meint drei Dinge. Nur zwei davon gehören in dieses Produkt (ADR-0023):

- **Vertrauen zwischen Control Plane und Agent** — bereits seit M1: host-gebundene
  Credentials, einmalige Enrollment-Token, verpflichtende menschliche Bestätigung,
  jederzeit widerrufbar. Ein Agent im selben LAN hat ohne Credential keinerlei Zugang.
- **Zugriff von Nutzern auf Hosts** — neu: Rolle plus Host-Beschränkung, geprüft bei
  jedem Aufruf, protokolliert mit Nutzernamen. Kein Aufruf ist erlaubt, weil er von
  innen kommt.
- **Netzwerkzugang zur Plattform** — *nicht* Teil des Produkts. Tailscale, WireGuard und
  Cloudflare Tunnel lösen das gut; wir integrieren sie ab v0.3, statt mit ihnen zu
  konkurrieren.

Ausdrücklich nicht im Modell: SSH-Zugang durch die Plattform. Wer eine Shell auf dem
Host hat, hat alles — das würde die gesamte Rechteprüfung aushebeln.

## Nachgewiesen

Die Rechte werden **auf HTTP-Ebene** getestet, nicht nur in der Bibliothek. Das ist der
Unterschied, auf den es ankommt: Die Prüflogik kann korrekt sein und die API trotzdem
offen, wenn eine Route die Middleware nicht durchläuft.

- Neun geschützte Routen ohne Anmeldung → alle 401
- viewer: liest (200), steuert nicht (403), sieht keine Nutzerliste (403), ändert kein Repo (403)
- viewer konnte keinen Admin anlegen — der Weg zur Rechteausweitung ist zu
- operator mit Host-Beschränkung: sieht nur seinen Host, fremder Host liefert **404
  statt 403** (die Existenz wird nicht verraten)
- API-Token eines viewers darf lesen, aber nicht steuern
- Rollenwechsel beendet bestehende Sitzungen — keine alten Rechte in alten Sitzungen
- Der letzte Admin kann sich weder herabstufen noch löschen
- Unbekannter Nutzer und falsches Passwort liefern identische Antworten
- Fehlversuche und Anmeldungen landen im Ereignisprotokoll

Git wird gegen **echte lokale Repos** getestet, nicht gegen einen nachgebauten Ablauf —
sonst würde genau das übergangen, was schiefgehen kann.

## Erkenntnisse aus dem Bau

1. **Das Ereignisprotokoll log ein Stück weit.** Zwei Einträge trugen den Auslöser
   `"user"` statt des Nutzernamens, weil der Enroller die Identität nicht kannte. Für
   ein Protokoll, das als Nachweis dienen soll, macht das den Zweck zunichte. Behoben:
   Der Auslöser wird durchgereicht.
2. **Ein Vorgang, zwei Einträge.** Die Host-Bestätigung wurde an zwei Stellen
   protokolliert. Doppelte Einträge machen ein Protokoll unlesbar. Jetzt schreibt nur
   noch der Enroller.
3. **Zeitausgleich bei unbekanntem Nutzer.** Ohne ihn verrät die Antwortzeit, welche
   Benutzernamen existieren — bei unbekanntem Nutzer wird deshalb gegen einen
   Platzhalter-Hash gerechnet.

## Bekannte Grenzen

- **TLS ist weiterhin optional.** Ohne Zertifikat laufen Passwörter im Klartext.
  Die Control Plane warnt beim Start; für Netzbetrieb ist es Pflicht.
- **Kein Ratenlimit bei der Anmeldung.** Fehlversuche werden protokolliert, aber nicht
  gebremst. Sollte vor dem ersten Release nachgezogen werden.
- **Kein Drift-Vergleich.** Der Soll-Zustand wird gelesen und angezeigt, aber noch nicht
  gegen den Ist-Zustand verglichen — das ist M4, das Herzstück.
- **Kein Webhook** für sofortige Synchronisation; der Takt liegt bei 60 Sekunden.
- **CGO für die Control Plane** bis zur Umstellung aus ADR-0020.

## Nächster Schritt: M4

Der semantische Vergleich: Soll aus Compose gegen Ist vom Agenten, normalisiert, mit
lesbarem Diff und den beiden Wegen `adopt` und `revert` (ADR-0004). Das ist das
eigentliche Produktmerkmal — und die Stelle, an der Falsch-positive das Vertrauen
schneller zerstören als jeder Absturz.
