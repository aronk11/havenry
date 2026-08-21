//go:build ignore

// Kleiner Docker-Daemon-Ersatz für manuelle Tests ohne installiertes Docker.
// Nur für die Entwicklung: go run scripts/devfake/main.go /tmp/fake-docker.sock
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

type container struct {
	ID, Name, Image, ImageID, State, Health string
	Labels                                  map[string]string
	Ports                                   []map[string]any
	Restart                                 int
}

var (
	mu         sync.Mutex
	containers = map[string]*container{
		"c1aaaaaaaaaa": {ID: "c1aaaaaaaaaa", Name: "media-jellyfin-1", Image: "jellyfin/jellyfin:10.9",
			ImageID: "sha256:aaa", State: "running", Health: "healthy",
			Labels: map[string]string{"com.docker.compose.project": "media", "com.docker.compose.service": "jellyfin"},
			Ports:  []map[string]any{{"PrivatePort": 8096, "PublicPort": 8096, "Type": "tcp"}}},
		"c2bbbbbbbbbb": {ID: "c2bbbbbbbbbb", Name: "media-sonarr-1", Image: "linuxserver/sonarr:4.0",
			ImageID: "sha256:bbb", State: "running", Restart: 5,
			Labels: map[string]string{"com.docker.compose.project": "media", "com.docker.compose.service": "sonarr"},
			Ports:  []map[string]any{{"PrivatePort": 8989, "PublicPort": 8989, "Type": "tcp"}}},
		"c3cccccccccc": {ID: "c3cccccccccc", Name: "proxy-caddy-1", Image: "caddy:2.8",
			ImageID: "sha256:ccc", State: "exited",
			Labels: map[string]string{"com.docker.compose.project": "proxy", "com.docker.compose.service": "caddy"}},
		"c4dddddddddd": {ID: "c4dddddddddd", Name: "handstart-test", Image: "alpine:3",
			ImageID: "sha256:ddd", State: "running", Labels: map[string]string{}},
	}
)

func main() {
	sock := "/tmp/fake-docker.sock"
	if len(os.Args) > 1 {
		sock = os.Args[1]
	}
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		panic(err)
	}
	fmt.Println("fake docker daemon auf", sock)
	panic(http.Serve(ln, http.HandlerFunc(handle)))
}

func handle(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if i := strings.Index(p[1:], "/"); i >= 0 {
		p = p[i+1:]
	}
	mu.Lock()
	defer mu.Unlock()

	switch {
	case p == "/version":
		json.NewEncoder(w).Encode(map[string]string{"Version": "24.0.7", "ApiVersion": "1.43", "Os": "linux", "Arch": "amd64"})
	case p == "/containers/json":
		out := []map[string]any{}
		for _, c := range containers {
			out = append(out, map[string]any{"Id": c.ID, "Names": []string{"/" + c.Name},
				"Image": c.Image, "ImageID": c.ImageID, "State": c.State, "Status": c.State,
				"Labels": c.Labels, "Ports": c.Ports})
		}
		json.NewEncoder(w).Encode(out)
	case strings.HasPrefix(p, "/containers/") && strings.HasSuffix(p, "/json"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/json")
		c, ok := containers[id]
		if !ok {
			notFound(w, id)
			return
		}
		st := map[string]any{"Status": c.State, "Running": c.State == "running"}
		if c.Health != "" {
			st["Health"] = map[string]any{"Status": c.Health}
		}
		json.NewEncoder(w).Encode(map[string]any{"Id": c.ID, "Name": "/" + c.Name,
			"RestartCount": c.Restart, "State": st,
			"Config": map[string]any{"Image": c.Image, "Labels": c.Labels}})
	case strings.HasSuffix(p, "/start"), strings.HasSuffix(p, "/stop"):
		target := "running"
		suffix := "/start"
		if strings.HasSuffix(p, "/stop") {
			target, suffix = "exited", "/stop"
		}
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), suffix)
		c, ok := containers[id]
		if !ok {
			notFound(w, id)
			return
		}
		if c.State == target {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		c.State = target
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(p, "/restart"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/restart")
		c, ok := containers[id]
		if !ok {
			notFound(w, id)
			return
		}
		c.State = "running"
		c.Restart++
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(p, "/logs"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/logs")
		c, ok := containers[id]
		if !ok {
			notFound(w, id)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		w.WriteHeader(http.StatusOK)
		for _, line := range []string{
			"[info] " + c.Name + " gestartet\n",
			"[info] lausche auf konfiguriertem port\n",
			"[warn] cache-verzeichnis nicht beschreibbar\n",
		} {
			w.Write(frame(1, line))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	default:
		notFound(w, p)
	}
}

func notFound(w http.ResponseWriter, id string) {
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"message": "No such container: " + id})
}

func frame(stream byte, text string) []byte {
	b := make([]byte, 8+len(text))
	b[0] = stream
	binary.BigEndian.PutUint32(b[4:], uint32(len(text)))
	copy(b[8:], text)
	return b
}
