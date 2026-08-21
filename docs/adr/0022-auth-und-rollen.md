# ADR-0022 — Nutzer, Rollen und Host-Beschränkung

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Bis M2 war die API offen: Wer die Control Plane erreichte, konnte Container
steuern. Für ein Werkzeug, das faktisch Root auf allen Hosts hat, ist das nicht
haltbar.

Gleichzeitig ist die Zielgruppe kein Unternehmen. Ein Homelab hat typischerweise
einen Betreiber und vielleicht zwei bis fünf weitere Personen: Partnerin, Mitbewohner,
ein Freund, der beim Medienserver hilft. Die Anforderung ist nicht Enterprise-RBAC,
sondern: *nicht jeder darf alles, und man sieht hinterher, wer was getan hat.*

## Entscheidung

**Drei Rollen**, bewusst wenige:

| Rolle | Darf |
|-------|------|
| `admin` | alles: Nutzer verwalten, Hosts bestätigen, Repo konfigurieren, Container steuern |
| `operator` | Container starten/stoppen/neustarten, Logs lesen — auf den erlaubten Hosts |
| `viewer` | nur lesen — auf den erlaubten Hosts |

**Host-Beschränkung.** Jeder Nutzer außer `admin` kann auf eine Menge von Hosts
beschränkt werden. Leere Menge bedeutet: alle Hosts. Das ist der eigentliche
Kern des Modells — der Mitbewohner sieht den Medienserver und sonst nichts.

**API-Token** für Automatisierung, an einen Nutzer gebunden, mit dessen Rechten
und eigener Ablaufzeit. Ein Token kann nie mehr dürfen als der Nutzer dahinter.

**Erster Start.** Ist kein Nutzer vorhanden, erzeugt die Control Plane einen
`admin` mit zufälligem Passwort und schreibt es einmalig ins Protokoll. Kein
Standardpasswort, kein offener Einrichtungsmodus, der vergessen werden kann.

**Passwörter** werden mit Argon2id gehasht (eigener Salt je Nutzer). Sitzungen
laufen über ein zufälliges Token im HttpOnly-Cookie; in der Datenbank steht nur
dessen Hash.

## Alternativen
- **Ein einziger Zugang ohne Nutzer:** einfacher, aber dann teilen sich alle ein
  Passwort und das Ereignisprotokoll nennt niemanden.
- **OIDC/SSO:** richtig für Firmen, für ein Homelab ein weiterer Dienst, der
  laufen und gewartet werden muss. Bleibt als späterer Zusatz möglich.
- **Feingranulare Rechte pro Stack:** mehr Ausdruckskraft, deutlich mehr
  Oberfläche und Erklärbedarf. Erst wenn jemand danach fragt.

## Konsequenzen
- Jede Aktion im Ereignisprotokoll trägt jetzt einen echten Nutzernamen als
  Auslöser (ADR-0018) — das macht das Protokoll erst zu einem Nachweis.
- Rechteprüfung passiert an genau einer Stelle (Middleware plus expliziter
  Host-Prüfung im Handler), nicht verstreut in der Oberfläche. Die Oberfläche
  blendet Knöpfe nur zusätzlich aus; die Prüfung im Server ist die verbindliche.
