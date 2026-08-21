package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aronk11/havenry/internal/transport"
)

// Test auf Agent-Ebene, nicht über den Transport.
//
// Eine frühere Fassung prüfte nur, ob die Abbestellung beim Agenten ankommt —
// das sagt nichts darüber aus, ob sie auch etwas bewirkt. Hier wird gezählt,
// wie viele Streams tatsächlich noch laufen.

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeDockerLogs bedient /logs mit einem Strom, der nicht endet — genau der
// Fall follow=true, in dem ein Leck dauerhaft wäre.
//
// Der Zähler offener Log-Verbindungen ist der eigentliche Nachweis: Die Zahl
// der Karteieinträge im Agenten sagt nichts darüber aus, ob die Goroutine
// samt Verbindung wirklich endet. Genau das war der Unterschied, an dem eine
// frühere Fassung dieses Tests vorbeigeprüft hat.
func fakeDockerLogs(t *testing.T) (socket string, offeneStreams func() int) {
	t.Helper()
	var offen atomic.Int64
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("unix-socket: %v", err)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case pathHasSuffix(r.URL.Path, "/logs"):
			offen.Add(1)
			defer offen.Add(-1)
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			// Endlos senden, bis die Gegenseite abbricht.
			for i := 0; i < 100000; i++ {
				if _, err := w.Write(frame(1, "zeile\n")); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{
				"Version": "24.0.7", "ApiVersion": "1.43",
			})
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sock, func() int { return int(offen.Load()) }
}

func pathHasSuffix(p, suffix string) bool {
	return len(p) >= len(suffix) && p[len(p)-len(suffix):] == suffix
}

func frame(stream byte, text string) []byte {
	b := make([]byte, 8+len(text))
	b[0] = stream
	binary.BigEndian.PutUint32(b[4:], uint32(len(text)))
	copy(b[8:], text)
	return b
}

// TestLogStreamIsReleasedOnUnsubscribe weist Fund A nach.
//
// Ohne Abbestellung liefe die Lese-Goroutine samt offener Docker-Verbindung
// unbegrenzt weiter, sobald der Browser die Log-Ansicht schließt. Jeder
// Log-Aufruf hinterließe eine Leiche.
func TestLogStreamIsReleasedOnUnsubscribe(t *testing.T) {
	sock, offeneStreams := fakeDockerLogs(t)

	a := New(Config{
		StateDir:     t.TempDir(),
		DockerSocket: sock,
		Logger:       quietLogger(),
	})
	// Ein Client wird nur benötigt, damit streamLogs nicht sofort aussteigt.
	a.client = transport.NewClient(transport.ClientConfig{
		ServerURL: "ws://127.0.0.1:1/agent",
		Logger:    quietLogger(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if n := a.ActiveLogStreams(); n != 0 {
		t.Fatalf("%d streams zu beginn", n)
	}

	go a.streamLogs(ctx, transport.LogSubscribe{
		SubID: "sub-1", ResourceID: "c1", TailLines: 10, Follow: true,
	})

	if !waitFor(func() bool { return a.ActiveLogStreams() == 1 }, 3*time.Second) {
		t.Fatal("der stream wurde nicht als laufend vermerkt")
	}

	if !waitFor(func() bool { return offeneStreams() == 1 }, 3*time.Second) {
		t.Fatal("es besteht keine log-verbindung zum daemon")
	}

	a.stopLogStream("sub-1")

	if !waitFor(func() bool { return a.ActiveLogStreams() == 0 }, 3*time.Second) {
		t.Fatalf("nach der abbestellung stehen noch %d einträge in der kartei",
			a.ActiveLogStreams())
	}
	// Der eigentliche Nachweis: Die Verbindung zum Daemon ist wirklich weg.
	if !waitFor(func() bool { return offeneStreams() == 0 }, 5*time.Second) {
		t.Fatal("die log-verbindung zum daemon läuft weiter — das leck besteht, " +
			"auch wenn die kartei leer aussieht")
	}
}

// TestDuplicateSubscriptionReplacesOld: Zwei Abonnements mit derselben ID
// dürfen sich nicht stapeln.
func TestDuplicateSubscriptionReplacesOld(t *testing.T) {
	sock, offeneStreams := fakeDockerLogs(t)

	a := New(Config{StateDir: t.TempDir(), DockerSocket: sock, Logger: quietLogger()})
	a.client = transport.NewClient(transport.ClientConfig{
		ServerURL: "ws://127.0.0.1:1/agent", Logger: quietLogger(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sub := transport.LogSubscribe{SubID: "sub-1", ResourceID: "c1", Follow: true}
	go a.streamLogs(ctx, sub)
	if !waitFor(func() bool { return a.ActiveLogStreams() == 1 }, 3*time.Second) {
		t.Fatal("erster stream nicht vermerkt")
	}

	go a.streamLogs(ctx, sub)
	time.Sleep(500 * time.Millisecond)

	if n := a.ActiveLogStreams(); n != 1 {
		t.Fatalf("%d streams nach doppeltem abonnement, erwartet 1", n)
	}

	a.stopLogStream("sub-1")
	if !waitFor(func() bool { return a.ActiveLogStreams() == 0 && offeneStreams() == 0 }, 5*time.Second) {
		t.Fatalf("nach abbestellung: %d einträge, %d offene verbindungen",
			a.ActiveLogStreams(), offeneStreams())
	}
}

// TestContextCancelReleasesStream: Auch ohne Abbestellung muss ein Stream
// enden, wenn die Verbindung wegfällt.
func TestContextCancelReleasesStream(t *testing.T) {
	sock, offeneStreams := fakeDockerLogs(t)

	a := New(Config{StateDir: t.TempDir(), DockerSocket: sock, Logger: quietLogger()})
	a.client = transport.NewClient(transport.ClientConfig{
		ServerURL: "ws://127.0.0.1:1/agent", Logger: quietLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go a.streamLogs(ctx, transport.LogSubscribe{
		SubID: "sub-1", ResourceID: "c1", Follow: true,
	})
	if !waitFor(func() bool { return a.ActiveLogStreams() == 1 }, 3*time.Second) {
		t.Fatal("stream nicht vermerkt")
	}

	cancel()
	if !waitFor(func() bool { return a.ActiveLogStreams() == 0 && offeneStreams() == 0 }, 5*time.Second) {
		t.Fatal("stream überlebte das ende der verbindung")
	}
}

func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
