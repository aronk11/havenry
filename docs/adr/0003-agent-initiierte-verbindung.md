# ADR-0003 — Agent-initiierte Outbound-Verbindung statt Push

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Homelab-Hosts sitzen hinter NAT, teils CGNAT, oft in getrennten VLANs oder an anderen
Standorten. Eingehende Verbindungen zu konfigurieren ist die größte Setup-Hürde
überhaupt und einer der meistgenannten Frustrationspunkte der Zielgruppe.

## Entscheidung
Der Agent baut eine dauerhafte ausgehende Verbindung zur Control Plane auf und holt sich
darüber Aufgaben ab. Die Control Plane verbindet sich niemals aktiv zu einem Agenten.
Kein offener Port auf dem Host, keine Firewall-Regel nötig.

## Alternativen
- **Control Plane pusht:** braucht erreichbare Agenten — für die Zielgruppe unrealistisch.
- **Reines Polling per HTTP:** einfacher, aber Latenz bei Logs und Live-Aktionen zu hoch.

## Konsequenzen
- Setup pro Host: ein Befehl, ein Token, fertig.
- Die Control Plane muss für die Agenten erreichbar sein — im MVP typischerweise im LAN
  oder über ein bestehendes Overlay-Netz (Tailscale). Eine gehostete Control Plane löst
  das später und ist ein natürlicher kommerzieller Ansatzpunkt (ADR-0010).
- Verbindungsabbrüche sind Normalzustand: Reconnect mit Backoff, Aufgaben idempotent.
