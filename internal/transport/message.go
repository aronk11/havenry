// Package transport implementiert das Agent-Protokoll aus ADR-0013:
// WebSocket, JSON, agent-initiiert (ADR-0003), Heartbeat und Reconnect.
package transport

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProtocolVersion wird getrennt von der Produktversion geführt und nur bei
// Breaking Changes erhöht (ADR-0016).
const ProtocolVersion = 1

// Nachrichtentypen des Protokolls v1. Siehe api/README.md.
const (
	TypeHello          = "hello"
	TypeHelloAck       = "hello.ack"
	TypeReportState    = "report.state"
	TypeReportMetrics  = "report.metrics"
	TypeCmdRequest     = "cmd.request"
	TypeCmdResult      = "cmd.result"
	TypeLogSubscribe   = "log.subscribe"
	TypeLogUnsubscribe = "log.unsubscribe"
	TypeLogChunk       = "log.chunk"
	TypePing           = "ping"
	TypePong           = "pong"
	TypeError          = "error"
)

// Envelope ist der äußere Rahmen jeder Nachricht. Payload bleibt roh, damit der
// Empfänger erst nach Typprüfung dekodiert.
type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope kodiert payload und verpackt es.
func NewEnvelope(typ, id string, payload any) (*Envelope, error) {
	e := &Envelope{Type: typ, ID: id}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("payload kodieren: %w", err)
		}
		e.Payload = b
	}
	return e, nil
}

// Decode entpackt die Payload in v.
func (e *Envelope) Decode(v any) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("nachricht %q hat keine payload", e.Type)
	}
	return json.Unmarshal(e.Payload, v)
}

// Hello ist die erste Nachricht des Agenten nach dem Verbindungsaufbau.
// Enthält entweder ein Enrollment-Token (Erstverbindung) oder ein
// Agent-Credential (alle weiteren Verbindungen) — siehe ADR-0015.
type Hello struct {
	ProtocolVersion int      `json:"protocol_version"`
	AgentVersion    string   `json:"agent_version"`
	Hostname        string   `json:"hostname"`
	EnrollToken     string   `json:"enroll_token,omitempty"`
	Credential      string   `json:"credential,omitempty"`
	Capabilities    []string `json:"capabilities"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
}

// HelloAck bestätigt die Verbindung. Bei einer Erstverbindung enthält es das
// dauerhafte Credential, das der Agent lokal ablegt.
type HelloAck struct {
	HostID string `json:"host_id"`
	// Credential ist nur bei der Erstverbindung gesetzt.
	Credential string `json:"credential,omitempty"`
	// Approved ist false, solange der Host in der UI nicht bestätigt wurde.
	// Ein unbestätigter Host darf melden, aber keine Kommandos ausführen (ADR-0015).
	Approved        bool          `json:"approved"`
	HeartbeatPeriod time.Duration `json:"heartbeat_period"`
	ReportPeriod    time.Duration `json:"report_period"`
}

// StateReport ist der vollständige Ist-Zustand eines Hosts.
// Bewusst vollständig statt inkrementell: die Datenmenge ist bei 1-20 Hosts
// unkritisch, und ein Vollbild ist gegen verlorene Nachrichten robust.
type StateReport struct {
	ObservedAt time.Time       `json:"observed_at"`
	Resources  []ResourceState `json:"resources"`
}

// ResourceState ist die Protokollform von provider.Resource. Bewusst dupliziert,
// damit das Drahtformat unabhängig vom internen Modell versioniert werden kann.
type ResourceState struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Stack  string `json:"stack,omitempty"`
	Image  string `json:"image,omitempty"`
	Digest string `json:"digest,omitempty"`
	State  string `json:"state"`
	Health string `json:"health,omitempty"`
	// Restart ist die Neustart-Regel des Containers. Ohne sie kann der
	// Vergleich sie nicht prüfen (ADR-0026).
	Restart  string            `json:"restart,omitempty"`
	Ports    []PortMapping     `json:"ports,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Restarts int               `json:"restarts,omitempty"`
}

type PortMapping struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}

// MetricsReport enthält Host-Metriken. Aufbewahrung siehe ADR-0018.
type MetricsReport struct {
	ObservedAt  time.Time `json:"observed_at"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemUsed     uint64    `json:"mem_used"`
	MemTotal    uint64    `json:"mem_total"`
	DiskUsed    uint64    `json:"disk_used"`
	DiskTotal   uint64    `json:"disk_total"`
	UptimeSecs  uint64    `json:"uptime_secs"`
	LoadAverage []float64 `json:"load_average,omitempty"`
}

// CmdRequest ist ein Kommando an einen Agenten.
//
// Jedes Kommando MUSS idempotent ausführbar sein: nach einem Verbindungsabbruch
// kann dieselbe CmdID erneut ankommen (ADR-0013).
type CmdRequest struct {
	CmdID      string            `json:"cmd_id"`
	Action     string            `json:"action"`
	ResourceID string            `json:"resource_id,omitempty"`
	Stack      string            `json:"stack,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	// ComposeYAML ist der Inhalt der Compose-Datei bei Stack-Aktionen.
	//
	// Bewusst der Inhalt und nicht ein Pfad: Der Agent bekommt keinen Zugriff
	// auf das Repo, dieses bleibt allein bei der Control Plane (ADR-0027).
	ComposeYAML string    `json:"compose_yaml,omitempty"`
	Deadline    time.Time `json:"deadline"`
}

// Zulässige Aktionen.
const (
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionPull    = "pull"

	// Stack-Aktionen über die Compose-CLI (ADR-0027).
	// ActionStackUp bringt einen Stack in den beschriebenen Zustand — das ist
	// die Umsetzung von "revert" und des Modus "apply" (ADR-0004).
	ActionStackUp   = "stack.up"
	ActionStackDown = "stack.down"
	ActionStackPull = "stack.pull"
)

// CmdResult meldet den Endzustand eines Kommandos. Jedes Kommando erreicht
// genau einen Endzustand — kein Kommando bleibt unbestimmt hängen (ADR-0013).
type CmdResult struct {
	CmdID   string `json:"cmd_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// Output ist die vollständige Ausgabe eines Compose-Aufrufs. Sie wird
	// unverändert durchgereicht, damit der Nutzer dasselbe sieht wie auf der
	// Kommandozeile (ADR-0027).
	Output   string `json:"output,omitempty"`
	Duration string `json:"duration,omitempty"`
}

const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusSkipped = "skipped" // bereits im Zielzustand — der Idempotenz-Fall
	StatusDenied  = "denied"  // Host nicht bestätigt (ADR-0015)
)

// LogSubscribe fordert einen Log-Stream an.
type LogSubscribe struct {
	SubID      string `json:"sub_id"`
	ResourceID string `json:"resource_id"`
	TailLines  int    `json:"tail_lines"`
	Follow     bool   `json:"follow"`
}

// LogUnsubscribe beendet einen Log-Stream.
//
// Ohne diese Nachricht liefe die Lese-Goroutine auf dem Agenten samt offener
// Docker-Verbindung weiter, sobald der Browser die Ansicht schließt — bei
// follow=true unbegrenzt. Jeder Log-Aufruf hinterließe eine Leiche.
type LogUnsubscribe struct {
	SubID string `json:"sub_id"`
}

// LogChunk transportiert Log-Daten. EOF beendet den Stream.
type LogChunk struct {
	SubID string `json:"sub_id"`
	Data  string `json:"data"`
	EOF   bool   `json:"eof,omitempty"`
}

// ErrorPayload wird bei Protokoll- oder Autorisierungsfehlern gesendet.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Fehlercodes. Fatal bedeutet: der Agent soll nicht blind weiter reconnecten.
const (
	ErrProtocolMismatch = "protocol_mismatch"
	ErrBadCredential    = "bad_credential"
	ErrTokenExpired     = "token_expired"
	ErrNotApproved      = "not_approved"
	ErrInternal         = "internal"
)

// IsFatal meldet, ob ein Fehlercode einen Reconnect sinnlos macht.
// Bei diesen Codes hilft kein Wiederholen — der Nutzer muss eingreifen.
func IsFatal(code string) bool {
	switch code {
	case ErrProtocolMismatch, ErrBadCredential, ErrTokenExpired:
		return true
	default:
		return false
	}
}
