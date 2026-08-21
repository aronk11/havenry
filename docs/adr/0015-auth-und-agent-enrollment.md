# ADR-0015 — Auth und Agent-Enrollment

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Die Plattform kann Container starten und stoppen und liest den Docker-Socket — sie ist
faktisch Root auf jedem Host. Gleichzeitig darf das Setup nicht abschrecken.

## Entscheidung

**Nutzer-Auth (MVP):** Ein einzelner lokaler Account, Passwort mit Argon2id gehasht,
Session-Cookie (HttpOnly, SameSite=Lax). Kein OIDC, kein Multi-User im MVP —
Schnittstelle ist aber so geschnitten, dass beides nachrüstbar ist.

**Agent-Enrollment:**
1. Nutzer erzeugt in der UI ein einmaliges, kurzlebiges Enrollment-Token (15 min).
2. Der Agent wird mit Token und Control-Plane-Adresse gestartet.
3. Beim ersten Verbinden tauscht er das Token gegen ein dauerhaftes, host-gebundenes
   Agent-Credential; das Enrollment-Token wird sofort entwertet.
4. Der Host erscheint in der UI und muss dort einmal bestätigt werden, bevor er
   Kommandos ausführen darf.

**Transportsicherheit:** TLS verpflichtend, außer bei explizitem `--insecure` für lokale
Tests. Selbstsignierte Zertifikate werden unterstützt; der Agent pinnt beim Enrollment
den Fingerprint (TOFU) und warnt bei Änderung.

## Konsequenzen
- Der Bestätigungsschritt in Schritt 4 verhindert, dass ein geleaktes Token still einen
  fremden Host anhängt.
- Docker-Socket-Zugriff ist ein reales Risiko und wird in der README offen benannt,
  nicht versteckt.
- Agent-Credentials sind widerrufbar; Widerruf trennt die Verbindung sofort.
