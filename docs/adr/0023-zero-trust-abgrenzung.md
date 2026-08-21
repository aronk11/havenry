# ADR-0023 — Zero Trust: was die Plattform übernimmt und was nicht

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
„Zero Trust" wird für drei verschiedene Dinge verwendet. Ohne Trennung baut man
entweder das Falsche oder verspricht etwas, das man nicht hält.

**(a) Netzwerkzugang zur Plattform** — wie erreicht jemand von unterwegs die
Control Plane, ohne Ports zu öffnen? Das lösen Tailscale, WireGuard und
Cloudflare Tunnel bereits gut, und die Zielgruppe nutzt sie ohnehin.

**(b) Vertrauen zwischen Control Plane und Agent** — jeder Host authentifiziert
sich einzeln, keine Verbindung ist allein aufgrund des Netzwerks vertrauenswürdig.

**(c) Zugriff von Nutzern auf Hosts durch die Plattform hindurch** — wer darf
auf welchem Host was, geprüft bei jedem einzelnen Aufruf statt einmal am
Netzwerkrand.

## Entscheidung

**(b) und (c) übernimmt die Plattform.**

Zu (b) ist das Wesentliche bereits umgesetzt: host-gebundene Credentials
(ADR-0015), einmalige Enrollment-Token, verpflichtende Bestätigung durch einen
Menschen, jederzeit widerrufbar, kein Netzwerkvertrauen — ein Agent im selben LAN
hat ohne Credential keinerlei Zugang.

Zu (c) liefert ADR-0022 das Modell: Rolle plus Host-Beschränkung, geprüft bei
jedem API-Aufruf, protokolliert mit Nutzernamen. Kein Aufruf ist deshalb erlaubt,
weil er von innen kommt.

**(a) übernimmt die Plattform nicht.** Wir bauen kein eigenes Overlay-Netz und
kein eigenes Tunnelprodukt. Ab v0.3 wird die *Einrichtung* bestehender Lösungen
geführt unterstützt (ADR-0008, `NetworkExposer`) — mehr nicht.

## Begründung
Ein eigenes Zugangsnetz zu bauen hieße, mit Tailscale und Cloudflare in deren
Kerndisziplin zu konkurrieren, mit einer Ein-Personen-Entwicklungskapazität und
in einem Bereich, in dem Fehler unmittelbar sicherheitsrelevant sind. Der
ehrliche Weg ist, das Vorhandene gut zu integrieren.

## Konsequenzen
- Die Plattform darf „Zero Trust" nur für (b) und (c) beanspruchen. In der
  Außendarstellung wird das benannt und nicht zu einem pauschalen
  Sicherheitsversprechen verkürzt.
- **Ausdrücklich nicht Teil des Modells:** SSH-Zugriff auf Hosts durch die
  Plattform. Ein eingebauter Terminal-Zugang würde die gesamte Rechteprüfung
  aushebeln — wer eine Shell hat, hat alles. Wer SSH will, nutzt SSH.
