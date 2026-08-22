package docker_test

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeDaemon ist ein minimaler Docker-Daemon auf einem Unix-Socket.
//
// Er existiert, weil der Provider sonst nur auf einem Rechner mit laufendem
// Docker testbar wäre — das würde bedeuten, dass die Fehlerpfade (Container
// verschwindet, 304 Not Modified, kaputter Log-Rahmen) faktisch nie geprüft
// werden. Genau diese Pfade sind aber die, die im Homelab auftreten.
type fakeDaemon struct {
	mu sync.Mutex

	containers map[string]*fakeContainer
	// logFrames wird bei /logs ausgeliefert.
	logFrames []byte
	// inspectFails lässt InspectContainer scheitern, um den Teilausfall zu prüfen.
	inspectFails bool

	srv      *http.Server
	socket   string
	listener net.Listener
}

type fakeContainer struct {
	ID      string
	Name    string
	Image   string
	ImageID string
	State   string
	Labels  map[string]string
	Ports   []map[string]any
	Health  string
	Restart int
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()

	// Unix-Socket-Pfade sind längenbegrenzt (~104 Zeichen); t.TempDir() ist
	// meist kurz genug, der kurze Dateiname hält Reserve.
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("unix-socket: %v", err)
	}

	d := &fakeDaemon{
		containers: make(map[string]*fakeContainer),
		socket:     sock,
		listener:   ln,
	}
	d.srv = &http.Server{Handler: http.HandlerFunc(d.handle)}
	go func() { _ = d.srv.Serve(ln) }()
	t.Cleanup(func() { _ = d.srv.Close() })
	return d
}

func (d *fakeDaemon) add(c *fakeContainer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers[c.ID] = c
}

func (d *fakeDaemon) state(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.containers[id]; ok {
		return c.State
	}
	return ""
}

func (d *fakeDaemon) handle(w http.ResponseWriter, r *http.Request) {
	// Pfad ohne Versionspräfix.
	path := r.URL.Path
	if i := strings.Index(path[1:], "/"); i >= 0 {
		path = path[i+1:]
	}

	switch {
	case path == "/version":
		writeJSON(w, map[string]string{
			"Version": "24.0.7", "ApiVersion": "1.43", "Os": "linux", "Arch": "arm64",
		})

	case path == "/containers/json":
		d.listContainers(w, r)

	case strings.HasSuffix(path, "/json") && strings.HasPrefix(path, "/containers/"):
		d.inspect(w, strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/json"))

	case strings.HasSuffix(path, "/start"):
		d.lifecycle(w, strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/start"), "running")

	case strings.HasSuffix(path, "/stop"):
		d.lifecycle(w, strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/stop"), "exited")

	case strings.HasSuffix(path, "/restart"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/restart")
		d.mu.Lock()
		c, ok := d.containers[id]
		if ok {
			c.State = "running"
			c.Restart++
		}
		d.mu.Unlock()
		if !ok {
			d.notFound(w, id)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case strings.HasSuffix(path, "/logs"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/logs")
		d.mu.Lock()
		_, ok := d.containers[id]
		frames := d.logFrames
		d.mu.Unlock()
		if !ok {
			d.notFound(w, id)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(frames)

	default:
		http.Error(w, `{"message":"nicht implementiert"}`, http.StatusNotFound)
	}
}

func (d *fakeDaemon) listContainers(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"

	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]map[string]any, 0, len(d.containers))
	for _, c := range d.containers {
		if !all && c.State != "running" {
			continue
		}
		out = append(out, map[string]any{
			"Id": c.ID, "Names": []string{"/" + c.Name},
			"Image": c.Image, "ImageID": c.ImageID,
			"State": c.State, "Status": c.State,
			"Labels": c.Labels, "Ports": c.Ports,
		})
	}
	writeJSON(w, out)
}

func (d *fakeDaemon) inspect(w http.ResponseWriter, id string) {
	d.mu.Lock()
	c, ok := d.containers[id]
	fails := d.inspectFails
	d.mu.Unlock()

	if fails {
		http.Error(w, `{"message":"inspect gestört"}`, http.StatusInternalServerError)
		return
	}
	if !ok {
		d.notFound(w, id)
		return
	}

	resp := map[string]any{
		"Id": c.ID, "Name": "/" + c.Name, "RestartCount": c.Restart,
		"State":  map[string]any{"Status": c.State, "Running": c.State == "running"},
		"Config": map[string]any{"Image": c.Image, "Labels": c.Labels},
	}
	if c.Health != "" {
		resp["State"].(map[string]any)["Health"] = map[string]any{"Status": c.Health}
	}
	writeJSON(w, resp)
}

// lifecycle bildet Dockers Verhalten nach: 304, wenn der Container bereits im
// Zielzustand ist. Genau darauf beruht die Idempotenz-Zusage.
func (d *fakeDaemon) lifecycle(w http.ResponseWriter, id, target string) {
	d.mu.Lock()
	c, ok := d.containers[id]
	if ok && c.State == target {
		d.mu.Unlock()
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if ok {
		c.State = target
	}
	d.mu.Unlock()

	if !ok {
		d.notFound(w, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *fakeDaemon) notFound(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "No such container: " + id})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// frame baut einen gerahmten Log-Abschnitt, wie Docker ihn ohne TTY liefert.
func frame(stream byte, text string) []byte {
	buf := make([]byte, 8+len(text))
	buf[0] = stream
	binary.BigEndian.PutUint32(buf[4:], uint32(len(text)))
	copy(buf[8:], text)
	return buf
}
