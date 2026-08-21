# ADR-0024 — TLS ist die Vorgabe, nicht die Option

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Bis M3 war TLS optional: Ohne Zertifikat lief die Control Plane im Klartext, mit
einer Warnung beim Start. Über diese Verbindung laufen Passwörter, Sitzungs-Cookies,
API-Token und Kommandos an alle Hosts.

Die Erfahrung mit optionalen Sicherheitsschaltern ist eindeutig: Was beim Einrichten
übersprungen werden kann, wird übersprungen — und dann jahrelang nicht nachgeholt.
Eine Warnung im Protokoll liest niemand, der das Werkzeug im Keller betreibt.

## Entscheidung
TLS ist eingeschaltet, sofern es nicht ausdrücklich abgeschaltet wird.

- Ohne konfiguriertes Zertifikat erzeugt die Control Plane beim ersten Start ein
  selbstsigniertes und legt es im Datenverzeichnis ab.
- Das Zertifikat enthält `localhost`, die Loopback-Adressen, den Hostnamen der
  Maschine, alle lokalen IPv4-Adressen und die über `--tls-names` ergänzten Namen.
- Der SHA-256-Fingerprint wird bei jedem Start protokolliert, damit er sich mit der
  Anzeige im Browser vergleichen lässt.
- Eigene Zertifikate (Let's Encrypt, interne CA) bleiben über `--tls-cert`/`--tls-key`
  möglich.
- `--no-tls` schaltet ab, mit einer unübersehbaren Warnung bei jedem Start.

**Laufzeit: zehn Jahre.** Bei öffentlichen Zertifikaten wären kurze Laufzeiten richtig;
hier wird ohnehin per Fingerprint vertraut (TOFU, wie beim Agenten in ADR-0015). Ein
Ablauf würde nur bedeuten, dass die Installation eines Tages ohne Zutun aufhört zu
funktionieren — der schlechteste denkbare Fehlermodus für ein Werkzeug, das Monate
nicht angefasst wird.

## Alternativen
- **ACME/Let's Encrypt eingebaut:** setzt eine öffentlich erreichbare Domain voraus.
  Die überwiegende Mehrheit der Zielgruppe betreibt das rein intern.
- **Weiter optional lassen:** in der Praxis gleichbedeutend mit „meistens aus".

## Konsequenzen
- Beim ersten Aufruf zeigt der Browser eine Zertifikatswarnung. Das ist bei
  selbstsignierten Zertifikaten unvermeidlich; der protokollierte Fingerprint erlaubt
  eine echte Prüfung statt blinden Wegklickens.
- Der Agent muss selbstsignierte Zertifikate akzeptieren können — das tut er bereits
  über Fingerprint-Pinning beim Enrollment (ADR-0015).
- Das Datenverzeichnis enthält jetzt einen privaten Schlüssel (Rechte 600). Wer es
  sichert, sichert damit auch diesen — in der Sicherungsanleitung zu erwähnen.
