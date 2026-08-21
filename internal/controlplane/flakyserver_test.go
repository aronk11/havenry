package controlplane_test

import (
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// flakyServer ist ein HTTP-Server, dessen TCP-Verbindungen sich gezielt kappen
// lassen — auch die von WebSocket gehijackten.
//
// Das ist nötig, weil httptest.Server.CloseClientConnections gehijackte
// Verbindungen nicht anfasst: ein WebSocket überlebt den Aufruf unbemerkt.
// Ohne diesen Wrapper testet man einen Ausfall, der gar keiner ist.
type flakyServer struct {
	URL string

	ln   net.Listener
	srv  *http.Server
	down chan struct{}

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	dead  bool
}

func newFlakyServer(t *testing.T, h http.Handler) *flakyServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}

	fs := &flakyServer{
		ln:    ln,
		conns: make(map[net.Conn]struct{}),
		URL:   "http://" + ln.Addr().String(),
	}

	fs.srv = &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ConnState: func(c net.Conn, state http.ConnState) {
			fs.mu.Lock()
			defer fs.mu.Unlock()
			switch state {
			case http.StateNew, http.StateActive, http.StateHijacked:
				fs.conns[c] = struct{}{}
			case http.StateClosed:
				delete(fs.conns, c)
			}
		},
	}

	go func() { _ = fs.srv.Serve(&gatedListener{Listener: ln, fs: fs}) }()

	t.Cleanup(func() {
		fs.mu.Lock()
		fs.dead = true
		fs.mu.Unlock()
		_ = fs.srv.Close()
	})
	return fs
}

// Outage kappt alle bestehenden Verbindungen und weist neue ab, bis Restore läuft.
func (fs *flakyServer) Outage() {
	fs.mu.Lock()
	fs.down = make(chan struct{})
	conns := make([]net.Conn, 0, len(fs.conns))
	for c := range fs.conns {
		conns = append(conns, c)
	}
	fs.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
}

// Restore lässt wieder Verbindungen zu.
func (fs *flakyServer) Restore() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.down != nil {
		close(fs.down)
		fs.down = nil
	}
}

func (fs *flakyServer) isDown() bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.down != nil
}

// gatedListener weist neue Verbindungen ab, solange ein Ausfall aktiv ist.
type gatedListener struct {
	net.Listener
	fs *flakyServer
}

func (l *gatedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.fs.isDown() {
			_ = c.Close()
			continue
		}
		return c, nil
	}
}
