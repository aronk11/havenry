# ADR-0001 — Docker/Compose als Zielplattform, nicht Kubernetes

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Ein erheblicher Teil der Homelab-Community ist von Kubernetes zurück zu Docker Compose
gewechselt. Der Grund ist nicht mangelndes Können, sondern Day-2-Aufwand: Cluster-Upgrades,
CNI-Probleme, Zertifikatsrotation, Storage. Compose löst 95 % der Homelab-Anwendungsfälle
mit einem Bruchteil der Wartung.

## Entscheidung
Die Plattform unterstützt ausschließlich Docker und Compose. Kein Kubernetes-Support,
auch nicht optional, auch nicht "später mal".

## Alternativen
- **Beides unterstützen:** verdoppelt die Reconcile-Logik, verwässert die Positionierung.
- **Kubernetes-first:** adressiert eine Zielgruppe, die bereits mit Flux/Argo gut versorgt ist.

## Konsequenzen
- Kleine, verständliche Codebasis; ein Zustandsmodell statt zwei.
- Klare Botschaft: "für Leute, die Compose nutzen und dabei bleiben wollen".
- Bewusster Verzicht auf das Enterprise-Segment.
- Podman bleibt als späterer Provider denkbar (siehe ADR-0008), weil API-kompatibel.
