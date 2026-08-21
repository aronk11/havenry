#!/usr/bin/env bash
# Wechselt den SQLite-Treiber von mattn/go-sqlite3 (CGO) auf modernc.org/sqlite
# (reines Go) — die Zielwahl aus ADR-0020.
#
# Warum das nicht schon geschehen ist: Beim Bau war modernc.org nicht
# erreichbar. Der Store spricht ausschließlich database/sql, deshalb ist der
# Wechsel eine Datei plus eine Konstante.
#
# Danach ist auch die Control Plane CGO-frei: statische Binaries und
# Cross-Compiling nach arm64 ohne Aufwand.
#
# Aufruf (einmalig, mit Netzzugang):
#   ./scripts/switch-to-pure-go-sqlite.sh
set -euo pipefail
cd "$(dirname "$0")/.."

if ! grep -q "mattn/go-sqlite3" internal/store/driver_cgo.go 2>/dev/null; then
  echo "Sieht bereits umgestellt aus — nichts zu tun."
  exit 0
fi

echo "1/5  Reinen Go-Treiber beziehen …"
go get modernc.org/sqlite@latest

echo "2/5  Treiberdatei ersetzen …"
rm -f internal/store/driver_cgo.go
cat > internal/store/driver.go <<'GO'
// Der SQLite-Treiber ist bewusst in eine eigene Datei isoliert.
//
// Der gesamte übrige Store-Code spricht nur database/sql und kennt keinen
// Treiber. Genau deshalb war der Wechsel von der CGO-Variante hierher ein
// Austausch dieser einen Datei — siehe ADR-0020.

package store

import _ "modernc.org/sqlite"

const driverName = "sqlite"
GO

echo "3/5  Makefile auf CGO-frei stellen …"
sed -i 's|^\tCGO_ENABLED=1 go build|\tCGO_ENABLED=0 go build|' Makefile
sed -i 's|^\tCGO_ENABLED=1 go test|\tgo test|' Makefile

echo "4/5  Dockerfile vereinfachen …"
sed -i 's|RUN apk add --no-cache gcc musl-dev git|RUN apk add --no-cache git|' Dockerfile
sed -i 's|CGO_ENABLED=1 go build|CGO_ENABLED=0 go build|' Dockerfile

echo "5/5  Aufräumen, bauen, testen …"
go mod tidy
go build ./...
go test ./... -count=1

cat <<'MSG'

Fertig. Beide Binaries sind jetzt CGO-frei.

Noch zu tun:
  - docs/adr/0020-sqlite-treiber.md: Status auf "Akzeptiert" ohne Vorbehalt
  - .github/workflows/ci.yml: CGO_ENABLED=1 aus dem Test-Schritt entfernen
MSG
