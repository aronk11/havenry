package transport

import (
	"encoding/json"
	"testing"
	"time"
)

// Das Protokoll ist der Vertrag zwischen zwei Programmen, die getrennt
// aktualisiert werden. Ein Feld, das beim Kodieren verschwindet, fällt sonst
// erst auf, wenn ein Agent im Keller etwas Falsches tut.
//
// Diese Tests prüfen bewusst das Drahtformat, nicht die Go-Typen: Was zählt,
// ist was durch die Leitung geht.

func TestEnvelopeRoundTrip(t *testing.T) {
	orig := CmdRequest{
		CmdID:      "cmd-1",
		Action:     ActionStackUp,
		Stack:      "media",
		ResourceID: "abc123",
		// Der Compose-Inhalt geht mit dem Kommando mit; der Agent hat keinen
		// Zugriff aufs Repo (ADR-0027).
		ComposeYAML: "services:\n  web:\n    image: nginx:1.27\n",
		Deadline:    time.Now().UTC().Truncate(time.Second),
	}

	env, err := NewEnvelope(TypeCmdRequest, orig.CmdID, orig)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}

	var back Envelope
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Type != TypeCmdRequest || back.ID != "cmd-1" {
		t.Fatalf("Rahmen falsch: %+v", back)
	}

	var got CmdRequest
	if err := back.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ComposeYAML != orig.ComposeYAML {
		t.Error("der Compose-Inhalt ging beim Kodieren verloren")
	}
	if got.Stack != orig.Stack || got.Action != orig.Action {
		t.Errorf("Felder verändert: %+v", got)
	}
	if !got.Deadline.Equal(orig.Deadline) {
		t.Errorf("Deadline = %v, erwartet %v", got.Deadline, orig.Deadline)
	}
}

// TestHelloCarriesCredentialOrToken: Beide Wege müssen durchs Drahtformat
// kommen — der eine für die Erstverbindung, der andere für jede weitere.
func TestHelloCarriesCredentialOrToken(t *testing.T) {
	for _, h := range []Hello{
		{ProtocolVersion: ProtocolVersion, Hostname: "nas-01", EnrollToken: "einmal"},
		{ProtocolVersion: ProtocolVersion, Hostname: "nas-01", Credential: "dauerhaft"},
	} {
		env, err := NewEnvelope(TypeHello, "", h)
		if err != nil {
			t.Fatal(err)
		}
		var got Hello
		if err := env.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.EnrollToken != h.EnrollToken || got.Credential != h.Credential {
			t.Fatalf("Zugangsdaten verändert: %+v", got)
		}
	}
}

// TestCredentialIsOmittedWhenEmpty: Leere Geheimnisfelder dürfen nicht im
// Drahtformat auftauchen. Sie stünden sonst in jedem Mitschnitt und in jedem
// Debug-Protokoll — als leeres Feld harmlos, als Gewohnheit nicht.
func TestSecretsAreOmittedWhenEmpty(t *testing.T) {
	env, err := NewEnvelope(TypeHello, "", Hello{
		ProtocolVersion: ProtocolVersion, Hostname: "nas-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := string(env.Payload)
	for _, field := range []string{"enroll_token", "credential"} {
		if contains(raw, field) {
			t.Errorf("leeres Feld %q steht im Drahtformat: %s", field, raw)
		}
	}
}

func TestDecodeRejectsEmptyPayload(t *testing.T) {
	env := &Envelope{Type: TypePing}
	var h Hello
	if err := env.Decode(&h); err == nil {
		t.Fatal("Decode ohne Payload wurde angenommen")
	}
}

// TestIsFatal hält fest, welche Ablehnungen einen Reconnect sinnlos machen.
//
// Die Unterscheidung ist wichtig: Bei einem fatalen Code muss der Agent
// aufgeben und es sagen. Bei allem anderen muss er weiter versuchen — ein
// Agent, der nach einem Netzwerkfehler aufgibt, ist im Homelab wertlos.
func TestIsFatal(t *testing.T) {
	fatal := []string{ErrProtocolMismatch, ErrBadCredential, ErrTokenExpired}
	for _, c := range fatal {
		if !IsFatal(c) {
			t.Errorf("%q sollte fatal sein — sonst reconnectet der Agent endlos", c)
		}
	}

	// Nicht fatal: Der Host wartet nur auf Bestätigung, das kann sich jederzeit
	// ändern. Ein interner Fehler kann vorübergehend sein.
	for _, c := range []string{ErrNotApproved, ErrInternal, "irgendwas-neues"} {
		if IsFatal(c) {
			t.Errorf("%q wurde als fatal gewertet — der Agent gäbe zu früh auf", c)
		}
	}
}

// TestUnknownErrorCodeIsNotFatal ist der Vorwärtskompatibilitätsfall: Ein
// neuer Server mit einem Fehlercode, den ein alter Agent nicht kennt, darf ihn
// nicht zum Aufgeben bringen.
func TestUnknownErrorCodeIsNotFatal(t *testing.T) {
	if IsFatal("code_aus_der_zukunft") {
		t.Fatal("ein unbekannter Fehlercode wurde als fatal gewertet")
	}
}

func TestCmdResultCarriesOutput(t *testing.T) {
	// Die Compose-Ausgabe wird unverändert durchgereicht (ADR-0027) — sie ist
	// für den Nutzer aussagekräftiger als jede Umschreibung.
	res := CmdResult{
		CmdID: "c1", Status: StatusFailed,
		Message: "docker compose up fehlgeschlagen",
		Output:  "Error response from daemon: pull access denied",
	}
	env, err := NewEnvelope(TypeCmdResult, res.CmdID, res)
	if err != nil {
		t.Fatal(err)
	}
	var got CmdResult
	if err := env.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Output != res.Output {
		t.Fatal("die Compose-Ausgabe ging verloren")
	}
}

func TestStateReportPreservesPortsAndLabels(t *testing.T) {
	r := StateReport{
		ObservedAt: time.Now().UTC().Truncate(time.Second),
		Resources: []ResourceState{{
			ID: "c1", Name: "media-web-1", Kind: "container",
			Stack: "media", Image: "nginx:1.27", State: "running",
			Restart: "unless-stopped",
			Ports:   []PortMapping{{Host: 8080, Container: 80, Protocol: "tcp"}},
			Labels:  map[string]string{"com.docker.compose.service": "web"},
		}},
	}

	env, err := NewEnvelope(TypeReportState, "", r)
	if err != nil {
		t.Fatal(err)
	}
	var got StateReport
	if err := env.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Resources) != 1 {
		t.Fatal("Ressource verloren")
	}
	res := got.Resources[0]
	// Diese drei entscheiden über den Drift-Vergleich. Geht eines verloren,
	// meldet die Plattform Abweichungen, die es nicht gibt.
	if len(res.Ports) != 1 || res.Ports[0].Host != 8080 {
		t.Errorf("Ports = %+v", res.Ports)
	}
	if res.Labels["com.docker.compose.service"] != "web" {
		t.Errorf("Labels = %v", res.Labels)
	}
	if res.Restart != "unless-stopped" {
		t.Errorf("Restart = %q", res.Restart)
	}
}

// TestProtocolVersionIsSet: Ein Hello ohne Version würde vom Server als
// Fehlversion abgelehnt.
func TestProtocolVersionConstant(t *testing.T) {
	if ProtocolVersion < 1 {
		t.Fatalf("ProtocolVersion = %d", ProtocolVersion)
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
