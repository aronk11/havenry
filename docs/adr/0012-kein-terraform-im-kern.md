# ADR-0012 — Kein Terraform/OpenTofu im Kern

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Naheliegender Gedanke: Terraform kann alles provisionieren — warum nicht die Plattform
darauf aufbauen? Die Idee wurde geprüft und verworfen.

## Entscheidung
Der Kern nutzt kein Terraform/OpenTofu. Die Docker-Ebene wird nativ über die Docker-API
verwaltet.

## Begründung
1. **Zweiter Source of Truth.** Terraform bringt eine State-File mit. Das widerspricht
   direkt ADR-0002 (Git ist die Wahrheit, die DB ist wegwerfbar). State-Drift,
   State-Locking und State-Korruption wären neue Pains für eine Zielgruppe ohne
   Remote-Backend und ohne Team.
2. **Gewicht.** Binary plus Provider-Plugins liegen schnell im dreistelligen MB-Bereich
   pro Host — unvereinbar mit den Budgets aus ADR-0005.
3. **Geschwindigkeit.** Ein `plan` über alle Stacks dauert Sekunden bis Minuten. Das
   Drift-Dashboard soll sich live anfühlen.
4. **Schwacher Docker-Provider.** Compose-Semantik lässt sich in HCL nur mühsam
   nachbilden. Wir würden ausgerechnet die Kernebene verschlechtern.
5. **Zielgruppen-Mismatch.** Wer von Kubernetes zu Compose zurückgekehrt ist, will kein
   weiteres deklaratives Ökosystem lernen. Compose *ist* das Format der Zielgruppe.

## Ausnahme
Auf der Infrastrukturebene ist der Einwand schwächer, weil VM-Provisioning einmalig statt
kontinuierlich ist. Ab v0.3+ ist **OpenTofu** (nicht Terraform, wegen der HashiCorp-
Lizenzänderung 2023) als optionaler Provider hinter ADR-0008 denkbar — opt-in, nie eine
Abhängigkeit, die alle Nutzer tragen.
