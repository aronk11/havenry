# ADR-0014 — Repo-Layout und Stack-zu-Host-Zuordnung

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Aus ADR-0002 folgt: Das Nutzer-Repo enthält gewöhnliche Compose-Dateien. Die Plattform
muss trotzdem wissen, welcher Stack auf welchem Host laufen soll — ohne ein eigenes
Format zu erfinden, das den Lock-in wieder einführt.

## Entscheidung
**Konvention mit optionaler Ergänzung.**

Konvention (Standardfall, keine Zusatzdatei nötig):
```
stacks/
  <hostname>/
    <stackname>/
      compose.yaml
      .env.example
```
Der Ordnername unterhalb von `stacks/` entspricht dem bei der Registrierung vergebenen
Hostnamen des Agenten.

Optional, für alles darüber hinaus, eine einzige Datei pro Stack:
```yaml
# stack.yaml — optional
hosts: [nas-01, nas-02]     # mehrere Ziele
mode: observe               # observe | apply, überschreibt den globalen Default
updates: notify             # notify | auto | off
health_window: 120s
```

Diese Datei ist rein additiv: Ohne sie funktioniert alles, und die Compose-Dateien
bleiben unverändert und ohne Plattform nutzbar.

## Alternativen
- **Labels in der Compose-Datei:** verändert die Datei des Nutzers — verstößt gegen ADR-0002.
- **Zuordnung nur in der DB:** unsichtbar in Git, nicht reproduzierbar.

## Konsequenzen
- Der Standardfall braucht null Konfiguration.
- `stack.yaml` ist der einzige plattformspezifische Artefakttyp und bleibt bewusst winzig.
- Monorepo-Nutzer mit abweichender Struktur können einen Basispfad konfigurieren.
