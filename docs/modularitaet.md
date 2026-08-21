# Modularität

Austauschbarkeit ist keine Frage von Schnittstellen allein. Ein Paket kann
lauter `interface`-Deklarationen enthalten und trotzdem festgenagelt sein —
wenn es keine Auswahlstelle gibt und keinen Nachweis, dass eine Alternative
dieselben Zusagen erfüllt.

## Die Datenbank

**Vorher austauschbar? Nein.** `internal/store` hatte Schnittstellen, aber:

- `cmd/havenry` rief `store.OpenSQLite` direkt auf
- Die Migrationen lagen in einer Paketvariablen, die zwei `init()`-Funktionen
  befüllten — globaler, reihenfolgeabhängiger Zustand im generischen Paket
- Die Konformitätstests kannten `*SQLite` als konkreten Typ

**Jetzt: drei Pakete** (ADR-0031)

```
internal/store/            Schnittstellen, Fehler, Registry — kennt keine Datenbank
internal/store/sqlitestore/  SQLite, registriert sich selbst
internal/store/memstore/     Arbeitsspeicher — der Nachweis
internal/store/storetest/    Konformitätssuite als wiederverwendbares Paket
```

Auswahl über eine DSN:

```bash
havenry --database sqlite:///var/lib/havenry/havenry.db
havenry --database memory://
havenry --database postgres://havenry@db.local/havenry   # noch nicht vorhanden
```

Ein blanker Pfad ohne Schema gilt weiter als SQLite — bestehende Aufrufe
funktionieren unverändert.

## Der Nachweis

Nach der Umstellung ist ein zweites Backend entstanden (`memstore`). Es
brauchte **keine Zeile Änderung an fremdem Code** und genau eine Testdatei:

```go
func TestConformance(t *testing.T) {
    storetest.Run(t, func(t *testing.T) store.Full { return memstore.New() })
}
```

Das ist der ganze Punkt. Ohne die Suite wäre „austauschbar" eine Behauptung.

`memstore` ist ausdrücklich **nicht für den Betrieb**: Ein Neustart verliert
alles. Es dient als Beleg und für schnelle Versuche.

## Die Suite prüft, was wirklich unterscheidet

Nicht „speichert es", sondern die Stellen, an denen Implementierungen
auseinanderlaufen:

- Benutzer- und Teamnamen eindeutig **und schreibungsunabhängig**
- Löschen eines Nutzers räumt Sitzungen, Token **und Mitgliedschaften** auf
- `ConsumeEnrollToken` atomar — 16 gleichzeitige Einlösungen, genau eine gewinnt
- Abgelaufene Sitzungen und Token gelten als nicht vorhanden
- Der Host-Upsert erhält `approved` und `enrolled_at`
- Eine leere Host-Liste bleibt leer (nicht `[""]`)
- Es gibt genau eine Repo-Konfiguration

**Sechs Mutationen gegen ein absichtlich kaputtes Backend, fünf sofort
gefangen.** Die sechste deckte eine echte Lücke in der Suite auf: Sie prüfte
nach dem Löschen eines Nutzers nur `TeamMembers` — und eine Implementierung,
die unbekannte Nutzer beim Auflisten stillschweigend überspringt, sieht damit
sauber aus, obwohl eine verwaiste Mitgliedschaft liegen bleibt. Sichtbar wird
sie erst, wenn ein Nutzer mit derselben ID neu entsteht und fremde Rechte erbt.
Die Suite prüft das jetzt.

## Was bewusst nicht abstrahiert wird

**Kein ORM, kein Query-Builder.** Jedes Backend schreibt sein eigenes SQL. Die
Unterschiede, auf die es ankommt — Upsert-Syntax, Groß-/Kleinschreibung,
Platzhalter, Zeitstempel — lassen sich nicht wegabstrahieren, nur verstecken.
Versteckt schlagen sie später als schwer auffindbare Verhaltensunterschiede zu.

## Andere Nahtstellen

| Was | Schnittstelle | Stand |
|---|---|---|
| Datenbank | `store.Full` + Registry | austauschbar, mit zwei Implementierungen belegt |
| Container-Laufzeit | `provider.Provider` mit Capability-Flags | eine Implementierung (Docker); Podman wäre API-nah |
| Soll-Zustand | `gitsync` | eine Quelle (Git) — bewusst, siehe ADR-0002 |
| Benachrichtigungen | `Notifier` | geplant für v0.2, noch keine Implementierung |

**Ehrlich zur Provider-Schnittstelle:** Sie existiert und hat Capability-Flags
(ADR-0008), aber es gibt nur eine Implementierung. Nach der Regel aus ADR-0008
gilt sie deshalb noch nicht als stabil — eine Schnittstelle, die um genau einen
Anwendungsfall herum entworfen wurde, ist erst dann erprobt, wenn eine zweite
sie erfüllt. Anders als beim Store ist das noch nicht geschehen.
