# ADR-0020 — SQLite-Treiber: reines Go statt CGO

**Status:** Akzeptiert (mit offener Umstellung) · **Datum:** 2026-08-19

## Kontext
ADR-0005 legt eine eingebettete SQLite-Datenbank fest. Für Go gibt es zwei ernsthafte Wege:

- **`github.com/mattn/go-sqlite3`** — Bindings an die C-Bibliothek. Ausgereift und schnell,
  erfordert aber CGO. Damit fällt `CGO_ENABLED=0` weg: statische Binaries und
  Cross-Compiling nach arm64 werden deutlich umständlicher. Für ein Projekt, dessen
  Zielgruppe zu großen Teilen auf Raspberry Pi und arm64-NAS läuft, ist das teuer.
- **`modernc.org/sqlite`** — SQLite nach Go übersetzt. Kein CGO, damit statische Binaries
  und triviales Cross-Compiling. Etwas langsamer, was bei dieser Last (wenige Schreibvorgänge
  pro Sekunde, 1–20 Hosts) irrelevant ist.

## Entscheidung
**`modernc.org/sqlite`** ist die Zielwahl: CGO-frei wiegt hier schwerer als Rohgeschwindigkeit.

**Aktueller Stand:** In der Entwicklungsumgebung, in der M2 gebaut wurde, war
`modernc.org` nicht erreichbar. Der Store wurde deshalb mit `mattn/go-sqlite3`
implementiert und getestet.

Damit die Umstellung ein Einzeiler bleibt, gilt:

- Der gesamte Store-Code spricht ausschließlich `database/sql` und handgeschriebenes SQL.
- Der Treiber-Import steht allein in `internal/store/driver_cgo.go`, zusammen mit der
  Konstante `driverName`.
- Es wird keine treiberspezifische Funktion verwendet.

Die Umstellung besteht aus: Datei ersetzen, `driverName` auf `"sqlite"` setzen,
`CGO_ENABLED=0` im Makefile wiederherstellen.

**Ausführbar hinterlegt:** `scripts/switch-to-pure-go-sqlite.sh` erledigt das in
einem Aufruf und baut und testet anschließend. Einmalig mit Netzzugang ausführen.

## Konsequenzen
- **Vorübergehend** braucht die Control Plane CGO. Das Agent-Binary bleibt CGO-frei —
  wichtiger Teilerfolg, denn der Agent läuft auf den vielen kleinen Hosts, die Control
  Plane nur einmal.
- Die Store-Tests laufen treiberunabhängig gegen die `Store`-Schnittstelle und gelten
  nach der Umstellung unverändert weiter.
- **Offen:** Umstellung vor dem ersten Release durchführen und diese ADR auf
  "Akzeptiert" ohne Vorbehalt setzen.
