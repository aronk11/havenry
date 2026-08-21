# ADR-0021 — Git über das `git`-Binary statt einer Bibliothek

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Die Control Plane muss das Nutzer-Repo klonen und aktuell halten (ADR-0002).
Zwei Wege: eine Go-Bibliothek (go-git) oder das `git`-Binary aufrufen.

## Entscheidung
Die Control Plane ruft das `git`-Binary auf.

## Begründung
- **Authentifizierung.** SSH-Deploy-Keys, `ssh-agent`, Credential-Helper,
  `~/.gitconfig`, Host-Key-Prüfung — all das funktioniert mit dem Binary sofort
  und exakt so, wie der Nutzer es von der Kommandozeile kennt. Eine Bibliothek
  bildet nur eine Teilmenge davon nach, und die Lücken zeigen sich erst bei
  echten Repos.
- **Größe.** go-git zieht einen erheblichen Abhängigkeitsbaum nach sich, ohne
  dass wir mehr als klonen, fetchen und auschecken brauchen.
- **Vertrautheit.** Schlägt etwas fehl, ist die Fehlermeldung die von `git` —
  die Nutzer dieser Zielgruppe können sie lesen und selbst beheben.

## Konsequenzen
- **`git` muss auf dem Host der Control Plane vorhanden sein.** Im Container-Image
  ist es enthalten; beim Betrieb als nacktes Binary muss es installiert sein.
  Fehlt es, meldet die Control Plane das beim Start als klaren Fehler, nicht als
  stillen Leerzustand.
- Alle Aufrufe laufen mit Zeitlimit und ohne Shell — Argumente werden als Liste
  übergeben, damit ein Repo-Pfad niemals als Befehl interpretiert werden kann.
- `GIT_TERMINAL_PROMPT=0` verhindert, dass ein Aufruf auf eine Passworteingabe
  wartet und dabei stillsteht.
