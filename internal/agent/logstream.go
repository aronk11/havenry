package agent

import (
	"context"
	"errors"
	"io"

	"github.com/aronk11/havenry/internal/transport"
)

// Log-Streams brauchen ein ausdrückliches Ende.
//
// Schließt der Browser die Ansicht, muss der Agent das erfahren. Ohne
// Abbestellung liefe die Lese-Goroutine samt offener Docker-Verbindung bei
// follow=true unbegrenzt weiter — jeder Log-Aufruf hinterließe eine Leiche.

// stopLogStream beendet ein Abonnement.
func (a *Agent) stopLogStream(subID string) {
	a.mu.Lock()
	ls, ok := a.logStreams[subID]
	if ok {
		delete(a.logStreams, subID)
	}
	a.mu.Unlock()
	if ok {
		ls.cancel()
	}
}

// ActiveLogStreams meldet die Zahl laufender Abonnements.
// Existiert für Tests: Ein Leck ist sonst nicht nachweisbar.
func (a *Agent) ActiveLogStreams() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.logStreams)
}

func (a *Agent) streamLogs(parent context.Context, sub transport.LogSubscribe) {
	a.mu.Lock()
	client := a.client
	if client == nil {
		a.mu.Unlock()
		return
	}
	// Ein zweites Abonnement mit derselben ID ersetzt das erste.
	if old, ok := a.logStreams[sub.SubID]; ok {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	mine := &logStream{cancel: cancel}
	a.logStreams[sub.SubID] = mine
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		// Nur den eigenen Eintrag entfernen: Hat inzwischen ein neues
		// Abonnement dieselbe ID belegt, gehört der Eintrag ihm.
		if cur, ok := a.logStreams[sub.SubID]; ok && cur == mine {
			delete(a.logStreams, sub.SubID)
		}
		a.mu.Unlock()
	}()

	sendChunk := func(c transport.LogChunk) {
		env, err := transport.NewEnvelope(transport.TypeLogChunk, sub.SubID, c)
		if err != nil {
			return
		}
		_ = client.Send(ctx, env)
	}

	stream, err := a.docker.Logs(ctx, sub.ResourceID, sub.TailLines, sub.Follow)
	if err != nil {
		sendChunk(transport.LogChunk{SubID: sub.SubID, Data: "logs nicht verfügbar: " + err.Error(), EOF: true})
		return
	}
	defer stream.Close() //nolint:errcheck

	for {
		if ctx.Err() != nil {
			return
		}
		entry, err := stream.Next()
		if errors.Is(err, io.EOF) {
			sendChunk(transport.LogChunk{SubID: sub.SubID, EOF: true})
			return
		}
		if err != nil {
			sendChunk(transport.LogChunk{SubID: sub.SubID, Data: "log-stream abgebrochen: " + err.Error(), EOF: true})
			return
		}
		if len(entry.Data) > 0 {
			sendChunk(transport.LogChunk{SubID: sub.SubID, Data: string(entry.Data)})
		}
	}
}
