# ADR-0006 — Kein eigenes Secrets-Management im MVP

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
"Secrets in Git" ist ein bekanntes, ungelöstes Problem. Es ordentlich zu lösen —
Verschlüsselung, Rotation, Key-Verwaltung — ist ein eigenes Produkt. Es schlecht zu
lösen ist schlimmer als es nicht zu lösen.

## Entscheidung
Das MVP verwaltet keine Secrets. Unterstützt wird:
- `.env`-Dateien, die **nicht** im Repo liegen, sondern pro Host lokal existieren und in
  der Compose-Datei referenziert werden.
- Die UI zeigt an, welche Variablen ein Stack erwartet und welche auf dem Host fehlen —
  ohne die Werte je anzuzeigen oder zu übertragen.

Kompatibilität mit SOPS/age wird durch die Reconcile-Schnittstelle vorbereitet
(Entschlüsselungs-Hook vor dem Vergleich), aber nicht implementiert.

## Alternativen
- **Eigene Secret-Verwaltung in der DB:** Sicherheitsversprechen, das wir nicht halten können.
- **SOPS direkt integrieren:** sinnvoll, aber v0.3 — nicht MVP.

## Konsequenzen
- Kein Sicherheitsversprechen ohne Deckung.
- Klar dokumentierte Grenze; die "fehlende Variable"-Anzeige löst schon einen echten
  Teil des Schmerzes (Stack startet nicht, niemand weiß warum).
