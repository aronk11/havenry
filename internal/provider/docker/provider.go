package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aronk11/havenry/internal/provider"
)

// Compose-Labels, die Docker Compose an jeden Container hängt. Über sie
// erkennt der Provider, welcher Container zu welchem Stack gehört — ohne dass
// der Nutzer irgendetwas annotieren muss (ADR-0002: die Compose-Datei bleibt unberührt).
const (
	LabelProject      = "com.docker.compose.project"
	LabelService      = "com.docker.compose.service"
	LabelConfigFiles  = "com.docker.compose.project.config_files"
	LabelWorkingDir   = "com.docker.compose.project.working_dir"
	LabelContainerNum = "com.docker.compose.container-number"
)

// DefaultStopTimeout ist die Kulanzfrist beim Stoppen.
const DefaultStopTimeout = 10 * time.Second

// Provider liest Container vom lokalen Docker-Daemon und steuert sie.
type Provider struct {
	c *Client
}

func New(socketPath string) *Provider {
	return &Provider{c: NewClient(socketPath)}
}

func (p *Provider) Name() string { return "docker" }

func (p *Provider) Capabilities() provider.Capability {
	return provider.CapRead | provider.CapLifecycle | provider.CapLogs
}

// Ping prüft die Erreichbarkeit des Daemons und liefert dessen Version.
// Wird beim Agent-Start aufgerufen, damit ein fehlender Socket sofort
// als klare Meldung auffällt und nicht als stiller Leerzustand.
func (p *Provider) Ping(ctx context.Context) (Version, error) {
	return p.c.Version(ctx)
}

// Observe liefert den vollständigen Ist-Zustand aller Container.
//
// Fehler bei einzelnen Containern brechen den Vorgang nicht ab: ein Container,
// der zwischen Auflisten und Inspizieren verschwindet, ist Normalfall und darf
// nicht dazu führen, dass der ganze Host als unbekannt gilt.
func (p *Provider) Observe(ctx context.Context) ([]provider.Resource, error) {
	containers, err := p.c.ListContainers(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("container auflisten: %w", err)
	}

	out := make([]provider.Resource, 0, len(containers))
	for _, ct := range containers {
		r := provider.Resource{
			ID:     ct.ID,
			Name:   containerName(ct.Names),
			Kind:   "container",
			Stack:  ct.Labels[LabelProject],
			Image:  ct.Image,
			Digest: ct.ImageID,
			State:  ct.State,
			Labels: ct.Labels,
		}
		for _, port := range ct.Ports {
			// Nur veröffentlichte Ports sind für die Drift-Erkennung relevant;
			// rein interne Ports stehen ohnehin im Image.
			if port.PublicPort == 0 {
				continue
			}
			r.Ports = append(r.Ports, provider.Port{
				Host: port.PublicPort, Container: port.PrivatePort, Protocol: port.Type,
			})
		}

		// Health und RestartCount stehen nur im Inspect. Schlägt das fehl,
		// bleibt der Rest trotzdem brauchbar.
		if ins, err := p.c.InspectContainer(ctx, ct.ID); err == nil {
			r.Restarts = ins.RestartCount
			if ins.State.Health != nil {
				r.Health = ins.State.Health.Status
			}
			if ins.Config.Image != "" {
				r.Image = ins.Config.Image
			}
			// Docker meldet eine fehlende Regel als leeren Namen oder "no".
			// Beides bedeutet dasselbe; die Vereinheitlichung passiert im
			// Vergleich, hier wird nur durchgereicht.
			r.Restart = ins.HostConfig.RestartPolicy.Name
			if r.Restart == "" {
				r.Restart = "no"
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// ErrUnknownAction wird bei einer nicht unterstützten Aktion geliefert.
var ErrUnknownAction = errors.New("unbekannte aktion")

// ActionOutcome unterscheidet "ausgeführt" von "war schon so".
type ActionOutcome int

const (
	OutcomeDone ActionOutcome = iota
	// OutcomeNoOp bedeutet: der Container war bereits im Zielzustand.
	// Das ist der Idempotenz-Fall aus ADR-0013 und kein Fehler.
	OutcomeNoOp
)

// Start startet einen Container. Bereits laufend → OutcomeNoOp.
func (p *Provider) Start(ctx context.Context, id string) (ActionOutcome, error) {
	err := p.c.StartContainer(ctx, id)
	return classify(err)
}

// Stop stoppt einen Container. Bereits gestoppt → OutcomeNoOp.
func (p *Provider) Stop(ctx context.Context, id string) (ActionOutcome, error) {
	err := p.c.StopContainer(ctx, id, DefaultStopTimeout)
	return classify(err)
}

// Restart startet einen Container neu. Ein Neustart ist immer eine Aktion,
// nie ein No-Op — auch bei einem gestoppten Container.
func (p *Provider) Restart(ctx context.Context, id string) (ActionOutcome, error) {
	if err := p.c.RestartContainer(ctx, id, DefaultStopTimeout); err != nil {
		return OutcomeDone, err
	}
	return OutcomeDone, nil
}

// classify übersetzt Docker-Antworten in ein Ergebnis.
//
// 304 Not Modified heißt: bereits im Zielzustand. Genau darauf beruht die
// Idempotenz-Zusage — ein doppelt zugestelltes Kommando ist harmlos.
func classify(err error) (ActionOutcome, error) {
	switch {
	case err == nil:
		return OutcomeDone, nil
	case IsNotModified(err):
		return OutcomeNoOp, nil
	default:
		return OutcomeDone, err
	}
}

// Logs öffnet einen Log-Stream für einen Container.
func (p *Provider) Logs(ctx context.Context, id string, tail int, follow bool) (LogStream, error) {
	rc, err := p.c.ContainerLogs(ctx, id, tail, follow)
	if err != nil {
		return nil, err
	}
	return newDemuxReader(rc), nil
}

func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	// Docker liefert Namen mit führendem Schrägstrich.
	return strings.TrimPrefix(names[0], "/")
}

var _ provider.Provider = (*Provider)(nil)
