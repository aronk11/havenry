# ADR-0031 — Austauschbare Datenbank

**Status:** Akzeptiert · **Datum:** 2026-08-20

## Kontext
`internal/store` definierte zwar Schnittstellen, aber es gab keine Naht, an der
sich etwas austauschen ließe:

- `cmd/havenry` rief `store.OpenSQLite` direkt auf. Eine andere Datenbank hätte
  bedeutet, den Aufrufer zu ändern.
- Die Migrationen lagen in einer Paketvariablen, die zwei `init()`-Funktionen
  befüllten. Ein globaler, reihenfolgeabhängiger Zustand im generischen Paket —
  und inhaltlich reines SQLite.
- Die Konformitätstests kannten `*SQLite` als konkreten Typ. Eine zweite
  Implementierung hätte nichts gehabt, woran sie sich messen könnte.

Schnittstellen allein machen nichts austauschbar. Austauschbar wird etwas erst
durch eine Auswahlstelle und einen Nachweis.

## Entscheidung

**Drei Pakete statt einem.**

- `internal/store` — Schnittstellen, Fehler und eine Registry. Kennt keine
  Datenbank.
- `internal/store/sqlitestore` — die SQLite-Implementierung samt eigener
  Migrationen. Registriert sich selbst.
- `internal/store/storetest` — die Konformitätssuite als **wiederverwendbares
  Paket**, nicht als Testdatei.

**Auswahl über eine DSN mit Schema.** `store.Open(ctx, dsn)` liest das Schema
und sucht das passende Backend:

```
sqlite:///var/lib/havenry/havenry.db
postgres://havenry@db.local/havenry      (noch nicht vorhanden)
```

Ein Backend meldet sich mit `store.Register(schema, opener)` an — im `init()`
seines eigenen Pakets, wo ein Nebeneffekt hingehört, weil er genau eine Sache
tut und nichts Fremdes verändert.

**Die Konformitätssuite ist der eigentliche Kern.** `storetest.Run(t, factory)`
führt jede Zusage gegen eine beliebige Implementierung aus. Ein neues Backend
braucht genau eine Testdatei:

```go
func TestConformance(t *testing.T) {
    storetest.Run(t, func(t *testing.T) store.Full { return openPostgres(t) })
}
```

Ohne diese Suite wäre „austauschbar" eine Behauptung. Mit ihr ist es eine
Prüfung, die entweder besteht oder nicht.

## Was ein Backend leisten muss
Die Suite prüft es, aber es steht auch hier, weil man es vor dem Anfangen wissen
will:

- **Benutzernamen und Teamnamen eindeutig, schreibungsunabhängig.** Sonst
  existieren „aron" und „Aron" nebeneinander.
- **Referenzen räumen auf.** Wird ein Nutzer gelöscht, verschwinden seine
  Sitzungen, Token und Mitgliedschaften. Ein gültiges Token eines gelöschten
  Nutzers ist ein Zugang, den niemand mehr sieht.
- **`ConsumeEnrollToken` ist atomar.** Prüfen und Entwerten in einem Schritt;
  zwei gleichzeitige Einlösungen desselben Tokens — genau eine gewinnt.
- **Abgelaufene Sitzungen und Token gelten als nicht vorhanden.** Die Prüfung
  gehört in die Abfrage, nicht in den Aufrufer.
- **Der Host-Upsert erhält `approved` und `enrolled_at`.** Eine Neuverbindung
  darf einen bestätigten Host nicht zurücksetzen.
- **Eine leere Host-Liste bleibt leer.** Käme sie als Liste mit einem leeren
  Eintrag zurück, hieße „alle Hosts" plötzlich „genau ein Host ohne Namen".
- **Es gibt genau eine Repo-Konfiguration.** Zweimal speichern erzeugt keine
  zweite Zeile.

## Was bewusst nicht abstrahiert wird
**Kein Query-Builder, kein ORM.** Jedes Backend schreibt sein eigenes SQL. Der
Grund: Die Unterschiede, auf die es ankommt — Upsert-Syntax, Groß-/Klein-
schreibung, Platzhalter, Zeitstempel — lassen sich nicht sinnvoll wegabstrahieren,
sondern nur verstecken. Versteckt schlagen sie später als schwer auffindbare
Verhaltensunterschiede zu. Zwei ehrliche Implementierungen sind besser als eine
undichte Abstraktion.

## Konsequenzen
- SQLite bleibt die Vorgabe. Ohne Angabe wird eine Datei im Datenverzeichnis
  benutzt, wie bisher (ADR-0005).
- Ein zweites Backend zu bauen bedeutet: ein Paket schreiben, `Register`
  aufrufen, `storetest.Run` bestehen. Kein Eingriff in fremden Code.
- Der Aufrufer sieht nur noch `store.Full`. Dass darunter SQLite liegt, weiß
  außer `main` niemand — und `main` nur, weil es die DSN zusammensetzt.
