# Mitarbeit

Dieses Projekt wird von einer Person gepflegt. Die Regeln hier sind deshalb
weniger Absprache als Gedächtnisstütze — für den Zustand in sechs Monaten,
wenn niemand mehr weiß, warum etwas so ist.

## Zum Stand: Beiträge von außen

**Pull Requests werden derzeit nicht angenommen.**

Nicht aus Unfreundlichkeit: Das Lizenzmodell ist noch nicht entschieden.
Solange ein einziger Urheber hinter dem Werk steht, bleiben alle Wege offen —
Open Source, gehostete Variante, beides. **Der erste angenommene fremde Beitrag
schließt Türen, die sich nicht wieder öffnen lassen**, weil eine Umlizenzierung
danach die Zustimmung aller Beitragenden bräuchte.

Was jederzeit willkommen ist und nichts verbaut:

- **Fehlermeldungen** — die sind wertvoller als Code
- **Diskussionen** zu Architektur und Richtung
- **Forks** für den Eigenbedarf, die AGPL erlaubt das ausdrücklich

Sobald die Frage entschieden ist, steht es hier.

## Bevor du Code schreibst

Lies die [ADRs](docs/adr/). Sie erklären nicht nur, *was* entschieden wurde,
sondern *warum* — und was bewusst **nicht** gebaut wird. Eine Änderung gegen
eine ADR ist nicht verboten, braucht aber eine neue ADR, die die alte ersetzt.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/). Die
Release-Automatik liest sie (ADR-0016), und die Historie ist die einzige Spur,
die von einer Begründung übrig bleibt.

```
<typ>(<bereich>): <beschreibung>

<warum — nicht was der Diff ohnehin zeigt>

Refs: ADR-XXXX
```

Regeln, die der Hook erzwingt:

- Betreff höchstens 72 Zeichen
- Beschreibung klein beginnen, kein Punkt am Ende
- Typ aus: `feat` `fix` `docs` `style` `refactor` `perf` `test` `build` `ci` `chore`
- `!` oder ein `BREAKING CHANGE:`-Absatz markiert brechende Änderungen

Einrichten:

```bash
./scripts/install-hooks.sh
```

Der Hook liegt in `.githooks/` und ist damit versioniert. `.git/hooks/` wird
beim Klonen nicht mitgenommen — ein Hook, den niemand hat, prüft nichts.

## Grundsätze

1. **Nichts, was den Nutzer überrascht.** Automatisch passiert nur, was
   ausdrücklich erlaubt wurde (ADR-0004).
2. **Leichtgewichtig ist ein Feature.** Die Größenbudgets aus ADR-0005 sind
   verbindlich und werden in CI geprüft.
3. **Keine neue Laufzeitabhängigkeit ohne ADR.**
4. **Falsch-positive Drifts sind Fehler erster Klasse.** Sie zerstören
   Vertrauen schneller als jeder Absturz.
5. **Ein Test, der beim ersten Lauf besteht, ist unbewiesen.** Bau einen Fehler
   ein und prüfe, ob der Test ihn fängt. Dieses Vorgehen hat hier mehrfach
   Tests entlarvt, die aus dem falschen Grund bestanden.

## Vor dem Commit

```bash
make test    # mit -race
gofmt -l .   # muss leer sein
go vet ./...
```

Der `pre-commit`-Hook prüft das ohnehin — er fängt ab, was sonst erst die
Pipeline meldet, nach dem Push, wenn man schon weitergedacht hat.

## Neue ADR

`docs/adr/template.md` kopieren, nächste freie Nummer, im Index eintragen.

Eine ADR gehört dorthin, wo eine Entscheidung getroffen wurde, die jemand
später anders treffen könnte — samt dem, was dagegen sprach. Ohne die
verworfenen Alternativen ist eine ADR nur eine Beschreibung.
