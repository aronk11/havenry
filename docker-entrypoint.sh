#!/bin/sh
# Baut die Kommandozeile aus Umgebungsvariablen, damit TLS in
# docker-compose.yaml per Variable an-/abgeschaltet werden kann, ohne die
# Compose-Datei selbst anzufassen (--no-tls existiert bereits als Flag,
# siehe cmd/havenry/main.go; hier nur bequem über ENV erreichbar gemacht).
#
# Wird ein explizites Kommando übergeben (z. B. eigenes `command:` in
# Compose), greift diese Logik nicht — dann läuft havenry genau mit den
# gegebenen Argumenten.
set -e

if [ "$#" -gt 0 ]; then
    exec /usr/local/bin/havenry "$@"
fi

set -- --addr="${HAVENRY_ADDR:-:8443}" --data-dir="${HAVENRY_DATA_DIR:-/var/lib/havenry}"

case "${HAVENRY_TLS:-on}" in
    off | false | 0 | no)
        set -- "$@" --no-tls
        ;;
    *)
        [ -n "$HAVENRY_TLS_CERT" ] && set -- "$@" --tls-cert="$HAVENRY_TLS_CERT"
        [ -n "$HAVENRY_TLS_KEY" ] && set -- "$@" --tls-key="$HAVENRY_TLS_KEY"
        [ -n "$HAVENRY_TLS_NAMES" ] && set -- "$@" --tls-names="$HAVENRY_TLS_NAMES"
        ;;
esac

exec /usr/local/bin/havenry "$@"
