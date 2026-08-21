// Der SQLite-Treiber ist bewusst in eine eigene Datei isoliert.
//
// Der gesamte übrige store.Store-Code spricht nur database/sql und kennt keinen
// Treiber. Der Wechsel auf den reinen Go-Treiber (modernc.org/sqlite) ist
// damit ein Einzeiler in genau dieser Datei — siehe ADR-0020.

package sqlitestore

import _ "github.com/mattn/go-sqlite3"

const driverName = "sqlite3"
