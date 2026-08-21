// Command controlplane ist der zentrale Dienst: er hält die Agent-Verbindungen,
// synchronisiert das Git-Repo, berechnet Drift und liefert API und Web-UI aus.
//
// Ein Binary, eingebettete Datenbank, keine externen Dienste (ADR-0005).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aronk11/havenry/internal/controlplane"
	"github.com/aronk11/havenry/internal/gitsync"
	"github.com/aronk11/havenry/internal/store"

	// Backends melden sich beim Import an (ADR-0031). Wer ein anderes will,
	// tauscht diese Zeile — im Aufrufer, nicht in der Bibliothek.
	_ "github.com/aronk11/havenry/internal/store/memstore"
	_ "github.com/aronk11/havenry/internal/store/sqlitestore"
)

var version = "dev"

func main() {
	var (
		addr         = flag.String("addr", ":8443", "Adresse für API, Web-UI und Agent-Verbindungen")
		dataDir      = flag.String("data-dir", "/var/lib/havenry", "Ablage für Datenbank und Repo-Cache")
		tlsCert      = flag.String("tls-cert", "", "Pfad zum TLS-Zertifikat (leer = selbstsigniertes erzeugen)")
		tlsKey       = flag.String("tls-key", "", "Pfad zum TLS-Schlüssel")
		tlsNames     = flag.String("tls-names", "", "Zusätzliche Hostnamen/IPs im Zertifikat, kommagetrennt")
		noTLS        = flag.Bool("no-tls", false, "TLS abschalten — NUR für lokale Tests, Passwörter laufen dann im Klartext")
		allowOrigins = flag.String("allow-origins", "",
			"Erlaubte Herkünfte für eine getrennt ausgelieferte Oberfläche, kommagetrennt")
		database = flag.String("database", "",
			"Datenbank-DSN, z. B. sqlite:///pfad/havenry.db (leer = Datei im data-dir)")
		debug   = flag.Bool("debug", false, "Ausführliche Protokollierung")
		showVer = flag.Bool("version", false, "Version ausgeben")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx0 := context.Background()

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		logger.Error("data-dir anlegen", "pfad", *dataDir, "fehler", err)
		os.Exit(1)
	}

	// Ohne Angabe eine Datei im Datenverzeichnis — wie bisher (ADR-0005).
	// Mit --database lässt sich ein anderes Backend wählen, sofern es
	// eingebunden ist (ADR-0031).
	dsn := *database
	if dsn == "" {
		dsn = filepath.Join(*dataDir, "havenry.db")
	}

	st, err := store.Open(ctx0, dsn)
	if err != nil {
		logger.Error("datenbank öffnen", "dsn", redactDSN(dsn), "fehler", err)
		os.Exit(1)
	}
	defer func() {
		if c, ok := st.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()
	logger.Info("datenbank geöffnet", "dsn", redactDSN(dsn), "backends", store.Backends())

	// git ist Voraussetzung fuer die Repo-Anbindung (ADR-0021). Fehlt es,
	// soll das beim Start auffallen und nicht erst, wenn jemand ein Repo setzt.
	if v, err := gitsync.CheckGitAvailable(); err != nil {
		logger.Warn("git nicht gefunden — die repo-anbindung bleibt unbenutzbar", "fehler", err)
	} else {
		logger.Info("git gefunden", "version", v)
	}

	workDir := filepath.Join(*dataDir, "repo")
	srv := controlplane.NewServer(st, version, workDir, logger)
	if *allowOrigins != "" {
		srv.SetAllowedOrigins(strings.Split(*allowOrigins, ","))
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		logger.Error("start fehlgeschlagen", "fehler", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	logger.Info("control plane startet", "version", version, "adresse", *addr)

	if *noTLS {
		// Bewusst laut und unmissverständlich: Wer das schaltet, soll es
		// bei jedem Start sehen.
		logger.Warn("!!! TLS IST ABGESCHALTET — passwörter und kommandos laufen im klartext !!!")
		logger.Warn("!!! nur für lokale tests verwenden, niemals im netzbetrieb        !!!")
		err = httpSrv.ListenAndServe()
	} else {
		cert, key := *tlsCert, *tlsKey
		if cert == "" || key == "" {
			var names []string
			if *tlsNames != "" {
				names = strings.Split(*tlsNames, ",")
			}
			if hn, e := os.Hostname(); e == nil {
				names = append(names, hn)
			}
			paths, fingerprint, e := controlplane.EnsureTLS(*dataDir, names)
			if e != nil {
				logger.Error("tls einrichten", "fehler", e)
				os.Exit(1)
			}
			cert, key = paths.Cert, paths.Key
			logger.Info("selbstsigniertes zertifikat aktiv", "pfad", paths.Cert)
			logger.Info("fingerprint (SHA-256) — im browser vergleichen:", "sha256", fingerprint)
		}
		err = httpSrv.ListenAndServeTLS(cert, key)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server beendet", "fehler", err)
		os.Exit(1)
	}
	logger.Info("control plane beendet")
}

// redactDSN entfernt Zugangsdaten aus einer DSN, bevor sie ins Protokoll geht.
//
// Bei einer Datei ist nichts zu verbergen, bei postgres://nutzer:geheim@host
// schon. Ein Passwort im Protokoll überlebt jede Rotation.
func redactDSN(dsn string) string {
	i := strings.Index(dsn, "://")
	if i < 0 {
		return dsn
	}
	rest := dsn[i+3:]
	at := strings.Index(rest, "@")
	if at < 0 {
		return dsn
	}
	return dsn[:i+3] + "***@" + rest[at+1:]
}
