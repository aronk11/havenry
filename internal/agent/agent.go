// Package agent enthält die Agent-Schleife: Credential-Verwaltung,
// Verbindungsaufbau, Zustandsmeldung und Kommando-Ausführung.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/aronk11/havenry/internal/provider"
	"github.com/aronk11/havenry/internal/provider/docker"
	"github.com/aronk11/havenry/internal/transport"
)

// Config konfiguriert den Agenten.
type Config struct {
	ServerURL    string
	EnrollToken  string
	StateDir     string
	DockerSocket string
	Version      string
	Insecure     bool
	Logger       *slog.Logger
}

// Agent verbindet den Host mit der Control Plane.
type Agent struct {
	cfg     Config
	docker  *docker.Provider
	compose *docker.Compose
	// composeOK meldet, ob die Compose-CLI vorhanden ist (ADR-0027).
	composeOK bool

	mu       sync.Mutex
	hostID   string
	approved bool
	client   *transport.Client
	metrics  metricsReader
	// logStreams hält je Log-Abonnement einen Eintrag. Ohne ihn liefe ein
	// Stream weiter, nachdem niemand mehr zuhört.
	logStreams map[string]*logStream
	// lastResults merkt sich Ergebnisse bereits ausgeführter Kommandos.
	// Kommt eine CmdID nach einem Verbindungsabbruch erneut an, wird das
	// gespeicherte Ergebnis wiederholt, statt die Aktion erneut auszuführen
	// (ADR-0013).
	lastResults map[string]transport.CmdResult
}

func New(cfg Config) *Agent {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DockerSocket == "" {
		cfg.DockerSocket = "/var/run/docker.sock"
	}
	return &Agent{
		cfg:         cfg,
		docker:      docker.New(cfg.DockerSocket),
		compose:     docker.NewCompose(filepath.Join(cfg.StateDir, "stacks"), cfg.DockerSocket),
		lastResults: make(map[string]transport.CmdResult),
		logStreams:  make(map[string]*logStream),
	}
}

// Run verbindet sich und bleibt verbunden, bis ctx endet.
func (a *Agent) Run(ctx context.Context) error {
	cred, err := a.loadCredential()
	if err != nil {
		return err
	}
	if cred.Credential == "" && a.cfg.EnrollToken == "" {
		return errors.New("dieser host ist nicht enrollt: --token angeben (token in der oberfläche erzeugen)")
	}
	if cred.Credential != "" && a.cfg.EnrollToken != "" {
		a.cfg.Logger.Info("host bereits enrollt — angegebenes token wird ignoriert")
	}

	// Docker früh prüfen: ein fehlender Socket ist der häufigste
	// Einrichtungsfehler und soll als klare Meldung erscheinen, nicht als
	// Host, der dauerhaft null Container meldet.
	if v, err := a.docker.Ping(ctx); err != nil {
		a.cfg.Logger.Error("docker nicht erreichbar — läuft der daemon, ist der socket eingebunden?",
			"socket", a.cfg.DockerSocket, "fehler", err)
	} else {
		a.cfg.Logger.Info("docker erreichbar", "version", v.Version, "api", v.APIVersion)
	}

	// Ohne Compose-CLI meldet der Agent die Fähigkeit apply gar nicht erst —
	// besser als Kommandos, die später unerklärt scheitern (ADR-0027).
	if v, ok := a.compose.Available(ctx); ok {
		a.composeOK = true
		a.cfg.Logger.Info("docker compose verfügbar", "version", v)
	} else {
		a.cfg.Logger.Warn("docker compose nicht gefunden — dieser host kann keine stacks anwenden",
			"folge", "revert und der modus apply stehen für diesen host nicht zur verfügung")
	}

	hostname, _ := os.Hostname()

	client := transport.NewClient(transport.ClientConfig{
		ServerURL: a.cfg.ServerURL,
		Insecure:  a.cfg.Insecure,
		Logger:    a.cfg.Logger,
		Hello: transport.Hello{
			AgentVersion: a.cfg.Version,
			Hostname:     hostname,
			EnrollToken:  a.cfg.EnrollToken,
			Credential:   cred.Credential,
			Capabilities: a.capabilities(),
			OS:           runtime.GOOS,
			Arch:         runtime.GOARCH,
		},
		OnCredential: a.saveCredential,
		OnConnected:  a.onConnected,
		Handler:      a.handle,
	})

	a.mu.Lock()
	a.client = client
	a.mu.Unlock()

	// Report-Schleife läuft neben der Verbindung. Sie meldet nur, wenn eine
	// Verbindung besteht — Send scheitert sonst folgenlos.
	reportCtx, stopReports := context.WithCancel(ctx)
	defer stopReports()
	go a.reportLoop(reportCtx)
	go a.metricsLoop(reportCtx)

	return client.Run(ctx)
}

func (a *Agent) onConnected(ack transport.HelloAck) {
	a.mu.Lock()
	a.hostID, a.approved = ack.HostID, ack.Approved
	a.mu.Unlock()

	if ack.Approved {
		a.cfg.Logger.Info("verbunden", "host_id", ack.HostID)
	} else {
		a.cfg.Logger.Warn("verbunden, aber noch nicht bestätigt — host in der oberfläche freigeben",
			"host_id", ack.HostID)
	}

	// Sofort einen Zustand melden, damit die Oberfläche nicht bis zum
	// nächsten Intervall leer bleibt.
	go a.sendState(context.Background())
}

// reportLoop meldet den Ist-Zustand in festem Takt.
func (a *Agent) reportLoop(ctx context.Context) {
	const period = 15 * time.Second
	t := time.NewTicker(period)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.sendState(ctx)
		}
	}
}

func (a *Agent) sendState(ctx context.Context) {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	if client == nil {
		return
	}

	obsCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	resources, err := a.docker.Observe(obsCtx)
	if err != nil {
		a.cfg.Logger.Warn("zustand erfassen", "fehler", err)
		return
	}

	report := transport.StateReport{
		ObservedAt: time.Now().UTC(),
		Resources:  make([]transport.ResourceState, 0, len(resources)),
	}
	for _, r := range resources {
		rs := transport.ResourceState{
			ID: r.ID, Name: r.Name, Kind: r.Kind, Stack: r.Stack,
			Image: r.Image, Digest: r.Digest, State: r.State,
			Health: r.Health, Labels: r.Labels, Restarts: r.Restarts,
		}
		for _, p := range r.Ports {
			rs.Ports = append(rs.Ports, transport.PortMapping{
				Host: p.Host, Container: p.Container, Protocol: p.Protocol,
			})
		}
		report.Resources = append(report.Resources, rs)
	}

	env, err := transport.NewEnvelope(transport.TypeReportState, "", report)
	if err != nil {
		return
	}
	if err := client.Send(ctx, env); err != nil {
		a.cfg.Logger.Debug("zustandsmeldung nicht zugestellt", "fehler", err)
	}
}

func (a *Agent) handle(ctx context.Context, env *transport.Envelope) (*transport.Envelope, error) {
	switch env.Type {
	case transport.TypeCmdRequest:
		var req transport.CmdRequest
		if err := env.Decode(&req); err != nil {
			return nil, err
		}
		res := a.execute(ctx, req)
		return transport.NewEnvelope(transport.TypeCmdResult, req.CmdID, res)

	case transport.TypeLogSubscribe:
		var sub transport.LogSubscribe
		if err := env.Decode(&sub); err != nil {
			return nil, err
		}
		go a.streamLogs(ctx, sub)
		return nil, nil

	case transport.TypeLogUnsubscribe:
		var unsub transport.LogUnsubscribe
		if err := env.Decode(&unsub); err != nil {
			return nil, err
		}
		a.stopLogStream(unsub.SubID)
		return nil, nil

	default:
		a.cfg.Logger.Debug("nachricht ignoriert", "typ", env.Type)
		return nil, nil
	}
}

// capabilities meldet, was dieser Host tatsächlich kann.
func (a *Agent) capabilities() []string {
	caps := capabilityNames(a.docker.Capabilities())
	if a.composeOK {
		caps = append(caps, "apply")
	}
	return caps
}

// metricsLoop meldet Host-Metriken.
func (a *Agent) metricsLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.sendMetrics(ctx)
		}
	}
}

func (a *Agent) sendMetrics(ctx context.Context) {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	if client == nil {
		return
	}

	m, err := a.metrics.Read(a.cfg.StateDir)
	if err != nil {
		a.cfg.Logger.Debug("metriken erfassen", "fehler", err)
		return
	}
	env, err := transport.NewEnvelope(transport.TypeReportMetrics, "", m)
	if err != nil {
		return
	}
	_ = client.Send(ctx, env)
}

// streamLogs liest Container-Logs und schickt sie abschnittsweise weiter.
// logStream ist ein laufendes Abonnement.
//
// Eigener Typ statt einer nackten CancelFunc, damit sich beim Aufräumen
// feststellen lässt, ob der Eintrag noch der eigene ist: Ersetzt ein zweites
// Abonnement mit derselben ID das erste, darf das verzögerte Aufräumen des
// alten nicht den Eintrag des neuen löschen. Derselbe Fehlertyp wie beim
// Sitzungswechsel im Hub.
type logStream struct {
	cancel context.CancelFunc
}

func capabilityNames(c provider.Capability) []string {
	var out []string
	if c&provider.CapRead != 0 {
		out = append(out, "read")
	}
	if c&provider.CapLifecycle != 0 {
		out = append(out, "lifecycle")
	}
	if c&provider.CapApply != 0 {
		out = append(out, "apply")
	}
	if c&provider.CapLogs != 0 {
		out = append(out, "logs")
	}
	return out
}
