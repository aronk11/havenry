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
      -o /out/havenry ./cmd/controlplane

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git openssh-client tzdata
RUN adduser -D -u 10001 homelab

COPY --from=build /out/havenry /usr/local/bin/havenry

# Enthält Datenbank, TLS-Schlüssel und die Arbeitskopie des Repos.
# Wer dieses Verzeichnis sichert, sichert auch den privaten Schlüssel.
VOLUME /var/lib/havenry
USER homelab
EXPOSE 8443

ENTRYPOINT ["/usr/local/bin/havenry"]
CMD ["--addr", ":8443", "--data-dir", "/var/lib/havenry"]
