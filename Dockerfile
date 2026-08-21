# Control Plane.
#
# CGO_ENABLED=1 wegen des SQLite-Treibers — vorübergehend, siehe ADR-0020.
# git wird zur Laufzeit gebraucht (ADR-0021), deshalb steht es im Endbild.
FROM golang:1.24-alpine AS build

RUN apk add --no-cache gcc musl-dev git
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/havenry ./cmd/havenry

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git openssh-client tzdata
RUN adduser -D -u 10001 homelab

COPY --from=build /out/havenry /usr/local/bin/havenry

# Enthält Datenbank, TLS-Schlüssel und die Arbeitskopie des Repos.
# Wer dieses Verzeichnis sichert, sichert auch den privaten Schlüssel.
# Erst anlegen und dem Laufzeitnutzer übergeben, dann VOLUME deklarieren —
# sonst initialisiert Docker das benannte Volume root-eigen und homelab
# (UID 10001) kann die Datenbank nicht öffnen.
RUN mkdir -p /var/lib/havenry && chown -R homelab:homelab /var/lib/havenry
VOLUME /var/lib/havenry

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

USER homelab
EXPOSE 8443

# Standardwerte entsprechen dem bisherigen Verhalten (TLS an, :8443,
# /var/lib/havenry). Per Umgebungsvariable übersteuerbar, siehe
# docker-entrypoint.sh — u. a. HAVENRY_TLS=off fürs Heimnetz ohne Zertifikate.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
