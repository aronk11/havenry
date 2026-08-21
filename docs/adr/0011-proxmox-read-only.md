# ADR-0011 — Proxmox nur lesend, kein VM-Lifecycle

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Bei vielen Homelabbern läuft Docker in VMs oder LXCs auf Proxmox. Eine Plattform, die nur
die Container-Ebene sieht, zeigt die halbe Wahrheit. Gleichzeitig unterscheidet sich die
Semantik grundlegend: Container sind wegwerfbar ("cattle"), VMs sind Pets — ihr Zustand
liegt auf Disks und lässt sich nicht durch Neuerstellen angleichen.

## Entscheidung
Proxmox wird ab v0.3 als **read-only Provider** unterstützt:
- Nodes, VMs, LXCs, deren Ressourcenverbrauch und Status werden angezeigt.
- Docker-Hosts werden ihrer VM/LXC und ihrem Node zugeordnet ("dieser Stack läuft in
  VM 104 auf pve-01") — das ist der eigentliche Mehrwert: eine durchgehende Sicht.
- **Kein** Erstellen, Löschen, Migrieren oder Reconcile von VMs.

## Alternativen
- **Voller VM-Lifecycle:** faktisch ein zweites Produkt mit eigener Zustandslogik.
- **Proxmox ignorieren:** verschenkt die Gesamtsicht, die den Unterschied macht.

## Konsequenzen
- Hoher wahrgenommener Wert bei geringen Kosten: ein API-Client, keine zweite
  Reconcile-Engine.
- Der `Provider`-Schnittstelle muss von Anfang an ansehen, dass es lesende und
  schreibende Provider gibt (Capability-Flags statt einer Methode, die manchmal
  "nicht unterstützt" wirft).
