package docker_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aronk11/havenry/internal/provider"
	"github.com/aronk11/havenry/internal/provider/docker"
)

func TestObserveMapsComposeStacks(t *testing.T) {
	d := newFakeDaemon(t)
	d.add(&fakeContainer{
		ID: "aaa111", Name: "media-jellyfin-1", Image: "jellyfin/jellyfin:10.9",
		ImageID: "sha256:abc", State: "running", Health: "healthy", Restart: 2,
		Labels: map[string]string{
			docker.LabelProject: "media",
			docker.LabelService: "jellyfin",
		},
		Ports: []map[string]any{
			{"IP": "0.0.0.0", "PrivatePort": 8096, "PublicPort": 8096, "Type": "tcp"},
			// Nicht veröffentlichter Port: darf nicht in der Drift-Sicht auftauchen.
			{"PrivatePort": 7359, "PublicPort": 0, "Type": "udp"},
		},
	})
	d.add(&fakeContainer{
		ID: "bbb222", Name: "einzelgaenger", Image: "alpine:3", State: "exited",
	})

	p := docker.New(d.socket)
	res, err := p.Observe(context.Background())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("%d ressourcen, erwartet 2 (auch gestoppte gehören dazu)", len(res))
	}

	byID := map[string]provider.Resource{}
	for _, r := range res {
		byID[r.ID] = r
	}

	jf := byID["aaa111"]
	if jf.Stack != "media" {
		t.Errorf("Stack = %q, erwartet %q — Compose-Label nicht ausgewertet", jf.Stack, "media")
	}
	if jf.Name != "media-jellyfin-1" {
		t.Errorf("Name = %q — führender Schrägstrich nicht entfernt?", jf.Name)
	}
	if jf.Health != "healthy" || jf.Restarts != 2 {
		t.Errorf("Health/Restarts aus Inspect fehlen: %+v", jf)
	}
	if len(jf.Ports) != 1 || jf.Ports[0].Host != 8096 {
		t.Errorf("Ports = %+v, erwartet nur den veröffentlichten Port", jf.Ports)
	}

	if byID["bbb222"].State != "exited" {
		t.Errorf("gestoppter Container fehlt oder hat falschen Zustand: %+v", byID["bbb222"])
	}
}

// TestObserveSurvivesInspectFailure prüft den Teilausfall: Wenn Inspect
// scheitert, muss der Rest trotzdem brauchbar sein. Ein Host darf nicht
// komplett als unbekannt gelten, nur weil ein Detailaufruf hakt.
func TestObserveSurvivesInspectFailure(t *testing.T) {
	d := newFakeDaemon(t)
	d.inspectFails = true
	d.add(&fakeContainer{
		ID: "aaa111", Name: "web", Image: "nginx:1.27", State: "running",
		Labels: map[string]string{docker.LabelProject: "proxy"},
	})

	res, err := docker.New(d.socket).Observe(context.Background())
	if err != nil {
		t.Fatalf("Observe darf bei Inspect-Fehler nicht scheitern: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("%d ressourcen, erwartet 1", len(res))
	}
	if res[0].Stack != "proxy" || res[0].State != "running" {
		t.Errorf("Grunddaten fehlen trotz erfolgreichem List: %+v", res[0])
	}
	if res[0].Health != "" {
		t.Errorf("Health sollte leer sein, wenn Inspect scheitert: %q", res[0].Health)
	}
}

// TestLifecycleIsIdempotent ist der Kern von ADR-0013: Ein doppelt
// zugestelltes Kommando darf nichts kaputt machen und muss als No-Op
// erkennbar sein.
func TestLifecycleIsIdempotent(t *testing.T) {
	d := newFakeDaemon(t)
	d.add(&fakeContainer{ID: "aaa111", Name: "web", Image: "nginx:1.27", State: "exited"})

	p := docker.New(d.socket)
	ctx := context.Background()

	out, err := p.Start(ctx, "aaa111")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if out != docker.OutcomeDone {
		t.Fatalf("erster Start = %v, erwartet OutcomeDone", out)
	}
	if d.state("aaa111") != "running" {
		t.Fatal("Container nicht gestartet")
	}

	// Wiederholung desselben Kommandos — der Fall nach einem Verbindungsabbruch.
	out, err = p.Start(ctx, "aaa111")
	if err != nil {
		t.Fatalf("wiederholter Start meldet Fehler statt No-Op: %v", err)
	}
	if out != docker.OutcomeNoOp {
		t.Fatalf("wiederholter Start = %v, erwartet OutcomeNoOp", out)
	}

	out, err = p.Stop(ctx, "aaa111")
	if err != nil || out != docker.OutcomeDone {
		t.Fatalf("Stop = %v, %v", out, err)
	}
	out, err = p.Stop(ctx, "aaa111")
	if err != nil || out != docker.OutcomeNoOp {
		t.Fatalf("wiederholter Stop = %v, %v — erwartet OutcomeNoOp", out, err)
	}
}

func TestActionOnMissingContainer(t *testing.T) {
	d := newFakeDaemon(t)
	p := docker.New(d.socket)

	_, err := p.Start(context.Background(), "gibt-es-nicht")
	if err == nil {
		t.Fatal("Start auf unbekanntem Container muss scheitern")
	}
	if !docker.IsNotFound(err) {
		t.Fatalf("IsNotFound erkennt den Fehler nicht: %v", err)
	}
	if !strings.Contains(err.Error(), "No such container") {
		t.Errorf("Fehlermeldung des Daemons ging verloren: %v", err)
	}
}

// TestLogDemuxStripsFraming ist der Test für die Stolperstelle: Ohne
// Entrahmung landen acht Bytes Kopfdaten als Steuerzeichen im Log-Text.
func TestLogDemuxStripsFraming(t *testing.T) {
	d := newFakeDaemon(t)
	d.add(&fakeContainer{ID: "aaa111", Name: "web", State: "running"})
	d.logFrames = append(append(
		frame(1, "starte server\n"),
		frame(2, "warnung: port belegt\n")...),
		frame(1, "bereit\n")...)

	stream, err := docker.New(d.socket).Logs(context.Background(), "aaa111", 100, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var text strings.Builder
	var sawStderr bool
	for {
		e, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if e.Stderr {
			sawStderr = true
		}
		text.Write(e.Data)
	}

	got := text.String()
	want := "starte server\nwarnung: port belegt\nbereit\n"
	if got != want {
		t.Fatalf("Log-Text = %q, erwartet %q", got, want)
	}
	if !sawStderr {
		t.Error("stderr-Abschnitt wurde nicht als solcher erkannt")
	}
	// Der Rahmenkopf beginnt mit einem Nullbyte — taucht es im Text auf,
	// wurde nicht entrahmt.
	if strings.ContainsRune(got, 0) {
		t.Error("Rahmenkopf ist im Log-Text gelandet")
	}
}

// TestLogDemuxHandlesTTY prüft den anderen Fall: TTY-Container liefern
// rohe Daten ohne Rahmen.
func TestLogDemuxHandlesTTY(t *testing.T) {
	d := newFakeDaemon(t)
	d.add(&fakeContainer{ID: "aaa111", Name: "web", State: "running"})
	d.logFrames = []byte("rohe ausgabe ohne rahmen\nzweite zeile\n")

	stream, err := docker.New(d.socket).Logs(context.Background(), "aaa111", 100, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var text strings.Builder
	for {
		e, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		text.Write(e.Data)
	}
	if text.String() != "rohe ausgabe ohne rahmen\nzweite zeile\n" {
		t.Fatalf("TTY-Text falsch entpackt: %q", text.String())
	}
}

func TestPingReportsDaemonVersion(t *testing.T) {
	d := newFakeDaemon(t)
	v, err := docker.New(d.socket).Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if v.Version != "24.0.7" || v.Arch != "arm64" {
		t.Fatalf("Version = %+v", v)
	}
}

func TestPingFailsClearlyWithoutDocker(t *testing.T) {
	// Der häufigste Einrichtungsfehler: Socket nicht eingebunden. Die Meldung
	// muss das benennen, statt als leerer Zustand zu erscheinen.
	_, err := docker.New("/nicht/vorhanden/docker.sock").Ping(context.Background())
	if err == nil {
		t.Fatal("Ping ohne Docker muss scheitern")
	}
	if !strings.Contains(err.Error(), "docker nicht erreichbar") {
		t.Fatalf("Fehlermeldung nicht hilfreich: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	caps := docker.New("/tmp/egal").Capabilities()
	for _, c := range []provider.Capability{provider.CapRead, provider.CapLifecycle, provider.CapLogs} {
		if caps&c == 0 {
			t.Errorf("Capability %v fehlt", c)
		}
	}
	// Apply kommt erst mit dem Reconciler in M4.
	if caps&provider.CapApply != 0 {
		t.Error("CapApply darf in M2 noch nicht gemeldet werden")
	}
}
