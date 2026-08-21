// Command agent läuft auf jedem Docker-Host, meldet den Ist-Zustand an die
// Control Plane und führt von dort erteilte Kommandos aus.
//
// Die Verbindung wird immer vom Agenten aufgebaut (ADR-0003) — kein offener
// Port auf dem Host.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aronk11/havenry/internal/agent"
	"github.com/aronk11/havenry/internal/transport"
)

var version = "dev"

func main() {
	var (
		server     = flag.String("server", "", "Adresse der Control Plane, z.B. wss://homelab.local:8443/agent")
		token      = flag.String("token", "", "Einmaliges Enrollment-Token (nur beim ersten Start, ADR-0015)")
		stateDir   = flag.String("state-dir", "/var/lib/havenry-agent", "Ablage für das Agent-Credential")
		dockerSock = flag.String("docker-socket", "/var/run/docker.sock", "Pfad zum Docker-Socket oder tcp://host:port")
		insecure   = flag.Bool("insecure", false, "TLS-Prüfung deaktivieren (nur für lokale Tests)")
		debug      = flag.Bool("debug", false, "Ausführliche Protokollierung")
		showVer    = flag.Bool("version", false, "Version ausgeben")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}
	if *server == "" {
		fmt.Fprintln(os.Stderr, "Fehler: --server ist erforderlich")
		flag.Usage()
		os.Exit(2)
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a := agent.New(agent.Config{
		ServerURL:    *server,
		EnrollToken:  *token,
		StateDir:     *stateDir,
		DockerSocket: *dockerSock,
		Version:      version,
		Insecure:     *insecure,
		Logger:       logger,
	})

	logger.Info("agent startet", "version", version, "server", *server)

	err := a.Run(ctx)
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		logger.Info("agent beendet")
	case errors.Is(err, transport.ErrFatal):
		// Dauerhafte Ablehnung — Wiederholen hilft nicht, der Nutzer muss handeln.
		logger.Error("verbindung dauerhaft abgelehnt", "fehler", err)
		os.Exit(1)
	default:
		logger.Error("agent beendet mit fehler", "fehler", err)
		os.Exit(1)
	}
}
