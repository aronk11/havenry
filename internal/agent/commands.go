package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/aronk11/havenry/internal/provider/docker"
	"github.com/aronk11/havenry/internal/transport"
)

// Kommandoausführung.
//
// Zwei Ebenen von Idempotenz (ADR-0013): Der Ergebnisspeicher fängt dieselbe
// CmdID nach einem Verbindungsabbruch ab, und die Aktionen selbst sind
// idempotent, weil Docker 304 meldet, wenn der Zielzustand schon besteht.

// execute führt ein Kommando aus.
//
// Doppelte Zustellung derselben CmdID führt nicht zu doppelter Ausführung:
// das gespeicherte Ergebnis wird wiederholt. Zusätzlich sind die Aktionen
// selbst idempotent (Docker meldet 304, wenn der Zielzustand schon besteht).
func (a *Agent) execute(ctx context.Context, req transport.CmdRequest) transport.CmdResult {
	a.mu.Lock()
	if prev, ok := a.lastResults[req.CmdID]; ok {
		a.mu.Unlock()
		a.cfg.Logger.Info("kommando bereits ausgeführt, ergebnis wiederholt", "cmd_id", req.CmdID)
		return prev
	}
	a.mu.Unlock()

	start := time.Now()
	res := a.runAction(ctx, req)
	res.Duration = time.Since(start).Round(time.Millisecond).String()

	a.mu.Lock()
	a.lastResults[req.CmdID] = res
	// Der Speicher darf nicht unbegrenzt wachsen. Bei dieser Kommandorate
	// reicht ein grobes Zurücksetzen völlig aus.
	if len(a.lastResults) > 512 {
		a.lastResults = map[string]transport.CmdResult{req.CmdID: res}
	}
	a.mu.Unlock()

	// Nach einer Zustandsänderung sofort melden, statt bis zum nächsten
	// Intervall zu warten — die Oberfläche soll unmittelbar reagieren.
	if res.Status == transport.StatusOK {
		go a.sendState(context.Background())
	}
	return res
}

func (a *Agent) runAction(ctx context.Context, req transport.CmdRequest) transport.CmdResult {
	ok := func(msg string) transport.CmdResult {
		return transport.CmdResult{CmdID: req.CmdID, Status: transport.StatusOK, Message: msg}
	}
	skipped := func(msg string) transport.CmdResult {
		return transport.CmdResult{CmdID: req.CmdID, Status: transport.StatusSkipped, Message: msg}
	}
	failed := func(err error) transport.CmdResult {
		return transport.CmdResult{CmdID: req.CmdID, Status: transport.StatusFailed, Message: err.Error()}
	}

	var (
		outcome docker.ActionOutcome
		err     error
	)
	switch req.Action {
	case transport.ActionStart:
		outcome, err = a.docker.Start(ctx, req.ResourceID)
	case transport.ActionStop:
		outcome, err = a.docker.Stop(ctx, req.ResourceID)
	case transport.ActionRestart:
		outcome, err = a.docker.Restart(ctx, req.ResourceID)
	case transport.ActionStackUp, transport.ActionStackDown, transport.ActionStackPull:
		return a.runStackAction(ctx, req)
	case transport.ActionPull:
		// Einzel-Image-Pull mit Digest-Auflösung folgt in M5 (ADR-0007);
		// für Stacks steht stack.pull zur Verfügung.
		return skipped("einzelner image-pull folgt in M5 — für stacks: stack.pull")
	default:
		return failed(fmt.Errorf("unbekannte aktion %q", req.Action))
	}

	switch {
	case err != nil && docker.IsNotFound(err):
		return failed(fmt.Errorf("container %q existiert nicht mehr", req.ResourceID))
	case err != nil:
		return failed(err)
	case outcome == docker.OutcomeNoOp:
		return skipped("war bereits im zielzustand")
	default:
		a.cfg.Logger.Info("kommando ausgeführt",
			"cmd_id", req.CmdID, "aktion", req.Action, "ressource", req.ResourceID)
		return ok("")
	}
}

// runStackAction führt eine Compose-Aktion aus (ADR-0027).
func (a *Agent) runStackAction(ctx context.Context, req transport.CmdRequest) transport.CmdResult {
	fail := func(msg string) transport.CmdResult {
		return transport.CmdResult{CmdID: req.CmdID, Status: transport.StatusFailed, Message: msg}
	}

	if !a.composeOK {
		return fail("docker compose ist auf diesem host nicht installiert (ADR-0027)")
	}
	if req.Stack == "" {
		return fail("stack-name fehlt")
	}
	if req.Action != transport.ActionStackDown && req.ComposeYAML == "" {
		return fail("compose-inhalt fehlt")
	}

	a.cfg.Logger.Info("stack-aktion", "cmd_id", req.CmdID, "aktion", req.Action, "stack", req.Stack)

	var (
		res docker.Result
		err error
	)
	switch req.Action {
	case transport.ActionStackUp:
		res, err = a.compose.Up(ctx, req.Stack, []byte(req.ComposeYAML))
	case transport.ActionStackPull:
		res, err = a.compose.Pull(ctx, req.Stack, []byte(req.ComposeYAML))
	case transport.ActionStackDown:
		res, err = a.compose.Down(ctx, req.Stack)
	}

	if err != nil {
		return transport.CmdResult{
			CmdID: req.CmdID, Status: transport.StatusFailed,
			Message: err.Error(), Output: res.Output,
		}
	}
	status := transport.StatusOK
	msg := ""
	if !res.Changed {
		// Nichts zu tun ist kein Fehler — der Zustand stimmte bereits.
		status = transport.StatusSkipped
		msg = "stack war bereits im gewünschten zustand"
	}
	return transport.CmdResult{
		CmdID: req.CmdID, Status: status, Message: msg, Output: res.Output,
	}
}
