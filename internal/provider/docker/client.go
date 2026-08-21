// Package docker spricht die Docker Engine API direkt über den Unix-Socket.
//
// Bewusst ohne das offizielle SDK: dessen Abhängigkeitsbaum ist groß genug, um
// die Größenbudgets aus ADR-0005 zu sprengen, und wir brauchen eine Handvoll
// Endpunkte. Ein eigener, kleiner Client macht außerdem sichtbar, was der Agent
// tatsächlich am Docker-Socket tut — bei einem Zugriff, der faktisch Root ist,
// ist das ein Vorteil und kein Selbstzweck.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIVersion ist die angefragte Engine-API-Version. Docker garantiert
// Abwärtskompatibilität, daher wird bewusst eine ältere Version gewählt:
// sie funktioniert auch auf lange nicht aktualisierten Homelab-Hosts.
const APIVersion = "v1.41"

// Client spricht mit dem Docker-Daemon.
type Client struct {
	http *http.Client
	// base ist ein Pseudo-Host; bei Unix-Sockets ignoriert der Dialer ihn.
	base string
}

// NewClient verbindet sich mit dem Docker-Socket.
//
// socketPath kann ein Unix-Socket (/var/run/docker.sock) oder eine
// TCP-Adresse (tcp://host:2375) sein.
func NewClient(socketPath string) *Client {
	if strings.HasPrefix(socketPath, "tcp://") {
		return &Client{
			http: &http.Client{Timeout: 0},
			base: "http://" + strings.TrimPrefix(socketPath, "tcp://"),
		}
	}
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
				// Der Agent spricht dauerhaft mit demselben Daemon.
				MaxIdleConns:    2,
				IdleConnTimeout: 90 * time.Second,
			},
		},
		base: "http://docker",
	}
}

// APIError ist eine Fehlerantwort des Daemons mit HTTP-Status.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("docker api: %d %s", e.StatusCode, e.Message)
}

// IsNotFound meldet, ob der Daemon die Ressource nicht kennt.
func IsNotFound(err error) bool {
	var ae *APIError
	if !asAPIError(err, &ae) {
		return false
	}
	return ae.StatusCode == http.StatusNotFound
}

// IsNotModified meldet den Idempotenz-Fall: Docker antwortet mit 304, wenn ein
// Container bereits im Zielzustand ist. Ein erneutes Start-Kommando ist damit
// kein Fehler, sondern ein No-Op — genau das verlangt ADR-0013.
func IsNotModified(err error) bool {
	var ae *APIError
	if !asAPIError(err, &ae) {
		return false
	}
	return ae.StatusCode == http.StatusNotModified
}

func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if ae, ok := err.(*APIError); ok { //nolint:errorlint // bewusst direkte Prüfung
			*target = ae
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	u := c.base + "/" + APIVersion + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker nicht erreichbar: %w", err)
	}

	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(body))

		// Der Daemon antwortet in der Regel als JSON mit einem message-Feld.
		var payload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Message != "" {
			msg = payload.Message
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// Version liefert Angaben zum Daemon — dient auch als Erreichbarkeitsprüfung.
type Version struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"`
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var v Version
	err := c.getJSON(ctx, "/version", nil, &v)
	return v, err
}

// Container ist ein Eintrag aus /containers/json.
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Ports   []ContainerPort   `json:"Ports"`
}

type ContainerPort struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// ListContainers listet Container. Mit all=true auch gestoppte — die sind für
// die Drift-Erkennung genauso wichtig wie laufende.
func (c *Client) ListContainers(ctx context.Context, all bool) ([]Container, error) {
	q := url.Values{}
	if all {
		q.Set("all", "1")
	}
	var out []Container
	err := c.getJSON(ctx, "/containers/json", q, &out)
	return out, err
}

// Inspect liefert Detailangaben zu einem Container, u.a. Health und RestartCount.
type Inspect struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Image string `json:"Image"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Restarting bool   `json:"Restarting"`
		StartedAt  string `json:"StartedAt"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	RestartCount int `json:"RestartCount"`
	Config       struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
		Env    []string          `json:"Env"`
	} `json:"Config"`
	HostConfig struct {
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
}

func (c *Client) InspectContainer(ctx context.Context, id string) (Inspect, error) {
	var out Inspect
	err := c.getJSON(ctx, "/containers/"+id+"/json", nil, &out)
	return out, err
}

// StartContainer startet einen Container.
// Ein bereits laufender Container liefert IsNotModified — kein Fehler.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// StopContainer stoppt einen Container mit Kulanzfrist.
// Ein bereits gestoppter Container liefert IsNotModified — kein Fehler.
func (c *Client) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	q := url.Values{}
	if timeout > 0 {
		q.Set("t", fmt.Sprintf("%d", int(timeout.Seconds())))
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/stop", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// RestartContainer startet einen Container neu.
func (c *Client) RestartContainer(ctx context.Context, id string, timeout time.Duration) error {
	q := url.Values{}
	if timeout > 0 {
		q.Set("t", fmt.Sprintf("%d", int(timeout.Seconds())))
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/restart", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ContainerLogs öffnet einen Log-Stream. Der Aufrufer schließt den Reader.
func (c *Client) ContainerLogs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("stdout", "1")
	q.Set("stderr", "1")
	if tail > 0 {
		q.Set("tail", fmt.Sprintf("%d", tail))
	} else {
		q.Set("tail", "100")
	}
	if follow {
		q.Set("follow", "1")
	}
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/logs", q)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
