package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Handler verarbeitet eingehende Nachrichten der Control Plane.
// Rückgabe einer Nachricht ist optional; nil bedeutet "keine Antwort".
type Handler func(ctx context.Context, env *Envelope) (*Envelope, error)

// ClientConfig konfiguriert den Agent-seitigen Transport.
type ClientConfig struct {
	// ServerURL ist die WebSocket-Adresse der Control Plane, z.B. wss://homelab.local:8443/agent
	ServerURL string
	// Hello wird bei jedem Verbindungsaufbau gesendet. Credential wird nach
	// erfolgreichem Enrollment von OnCredential gesetzt.
	Hello Hello
	// Insecure deaktiviert die TLS-Prüfung — nur für lokale Tests (ADR-0015).
	Insecure bool
	// OnCredential wird aufgerufen, wenn die Control Plane ein dauerhaftes
	// Credential ausstellt. Der Aufrufer persistiert es.
	OnCredential func(cred string) error
	// OnConnected meldet den bestätigten Verbindungsaufbau.
	OnConnected func(ack HelloAck)
	// Handler verarbeitet Kommandos und Log-Anfragen.
	Handler Handler
	Logger  *slog.Logger

	// Backoff-Parameter. Nullwerte werden mit sinnvollen Defaults belegt.
	BackoffMin time.Duration
	BackoffMax time.Duration
}

func (c *ClientConfig) applyDefaults() {
	if c.BackoffMin == 0 {
		c.BackoffMin = time.Second
	}
	if c.BackoffMax == 0 {
		c.BackoffMax = 2 * time.Minute
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Client hält die Verbindung zur Control Plane offen und stellt sie nach
// Abbrüchen selbstständig wieder her. Verbindungsabbrüche sind der
// Normalzustand im Homelab, kein Fehlerfall (ADR-0013).
type Client struct {
	cfg ClientConfig

	mu   sync.Mutex
	conn *websocket.Conn

	// fatal wird gesetzt, wenn ein Reconnect zwecklos ist.
	fatalErr error
}

func NewClient(cfg ClientConfig) *Client {
	cfg.applyDefaults()
	return &Client{cfg: cfg}
}

// ErrFatal signalisiert, dass ein Wiederverbinden nicht hilft und der
// Nutzer eingreifen muss.
var ErrFatal = errors.New("verbindung dauerhaft abgelehnt")

// Run verbindet sich und hält die Verbindung, bis ctx endet oder ein fataler
// Fehler auftritt. Nicht-fatale Fehler führen zu einem Reconnect mit
// exponentiellem Backoff plus Jitter — Jitter verhindert, dass nach einem
// Neustart der Control Plane alle Agenten gleichzeitig anklopfen.
func (c *Client) Run(ctx context.Context) error {
	attempt := 0
	for {
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.fatalErr != nil {
			return fmt.Errorf("%w: %w", ErrFatal, c.fatalErr)
		}

		attempt++
		wait := c.backoff(attempt)
		c.cfg.Logger.Warn("verbindung verloren, neuer versuch",
			"fehler", err, "versuch", attempt, "wartezeit", wait.Round(time.Second))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// backoff liefert exponentiell wachsende Wartezeit mit ±20 % Jitter.
func (c *Client) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exp := float64(c.cfg.BackoffMin) * math.Pow(2, float64(attempt-1))
	if exp > float64(c.cfg.BackoffMax) {
		exp = float64(c.cfg.BackoffMax)
	}
	jitter := 1 + (rand.Float64()*0.4 - 0.2) //nolint:gosec // kein Sicherheitskontext
	return time.Duration(exp * jitter)
}

// session baut eine Verbindung auf und bedient sie, bis sie endet.
func (c *Client) session(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	opts := &websocket.DialOptions{}
	if c.cfg.Insecure {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // bewusst, nur mit --insecure
			},
		}
	}

	conn, resp, err := websocket.Dial(ctx, c.cfg.ServerURL, opts)
	if resp != nil && resp.Body != nil {
		// Der Upgrade-Handshake hinterlässt eine http.Response, die die
		// websocket-Bibliothek selbst nicht schließt — ohne das hier bleibt
		// bei jedem (Re-)Connect eine Verbindung im TIME_WAIT hängen.
		// resp kann bei einem gescheiterten Handshake non-nil sein, ohne
		// dass Body gesetzt ist — deshalb beide Prüfungen (siehe
		// TestEnrollmentFlow, das ohne die zweite mit nil pointer abstürzte).
		defer resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("verbindungsaufbau: %w", err)
	}
	defer conn.CloseNow() //nolint:errcheck

	// Nachrichten können groß werden (Log-Chunks, vollständige State-Reports).
	conn.SetReadLimit(8 << 20)

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	ack, err := c.handshake(ctx, conn)
	if err != nil {
		return err
	}
	if c.cfg.OnConnected != nil {
		c.cfg.OnConnected(ack)
	}

	period := ack.HeartbeatPeriod
	if period <= 0 {
		period = 20 * time.Second
	}

	errc := make(chan error, 2)
	go func() { errc <- c.readLoop(ctx, conn) }()
	go func() { errc <- c.heartbeatLoop(ctx, conn, period) }()

	return <-errc
}

// handshake sendet Hello und wartet auf HelloAck. Ein Fehler mit fatalem Code
// beendet den Client, statt in eine endlose Reconnect-Schleife zu laufen.
func (c *Client) handshake(ctx context.Context, conn *websocket.Conn) (HelloAck, error) {
	hello := c.cfg.Hello
	hello.ProtocolVersion = ProtocolVersion

	env, err := NewEnvelope(TypeHello, "", hello)
	if err != nil {
		return HelloAck{}, err
	}
	if err := writeEnvelope(ctx, conn, env); err != nil {
		return HelloAck{}, fmt.Errorf("hello senden: %w", err)
	}

	hsCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := readEnvelope(hsCtx, conn)
	if err != nil {
		return HelloAck{}, fmt.Errorf("hello.ack empfangen: %w", err)
	}

	switch resp.Type {
	case TypeHelloAck:
		var ack HelloAck
		if err := resp.Decode(&ack); err != nil {
			return HelloAck{}, err
		}
		// Erstverbindung: Credential persistieren und für künftige Verbindungen
		// das Enrollment-Token verwerfen.
		if ack.Credential != "" && c.cfg.OnCredential != nil {
			if err := c.cfg.OnCredential(ack.Credential); err != nil {
				return HelloAck{}, fmt.Errorf("credential ablegen: %w", err)
			}
			c.cfg.Hello.Credential = ack.Credential
			c.cfg.Hello.EnrollToken = ""
		}
		if !ack.Approved {
			c.cfg.Logger.Warn("host wartet auf bestätigung in der oberfläche — kommandos werden abgelehnt")
		}
		return ack, nil

	case TypeError:
		var e ErrorPayload
		_ = resp.Decode(&e)
		if IsFatal(e.Code) {
			c.fatalErr = fmt.Errorf("%s: %s", e.Code, e.Message)
		}
		return HelloAck{}, fmt.Errorf("abgelehnt: %s (%s)", e.Message, e.Code)

	default:
		return HelloAck{}, fmt.Errorf("unerwartete antwort auf hello: %q", resp.Type)
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		env, err := readEnvelope(ctx, conn)
		if err != nil {
			return err
		}

		switch env.Type {
		case TypePing:
			pong, _ := NewEnvelope(TypePong, env.ID, nil)
			if err := writeEnvelope(ctx, conn, pong); err != nil {
				return err
			}
			continue
		case TypePong:
			continue
		}

		if c.cfg.Handler == nil {
			continue
		}

		// Kommandos nebenläufig bearbeiten, damit ein langsames Kommando
		// (Pull eines großen Images) den Kanal nicht blockiert.
		go func(env *Envelope) {
			reply, err := c.cfg.Handler(ctx, env)
			if err != nil {
				c.cfg.Logger.Error("nachricht verarbeiten", "typ", env.Type, "fehler", err)
				return
			}
			if reply == nil {
				return
			}
			if err := c.Send(ctx, reply); err != nil {
				c.cfg.Logger.Error("antwort senden", "typ", reply.Type, "fehler", err)
			}
		}(env)
	}
}

func (c *Client) heartbeatLoop(ctx context.Context, conn *websocket.Conn, period time.Duration) error {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			ping, _ := NewEnvelope(TypePing, "", nil)
			if err := writeEnvelope(ctx, conn, ping); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}
	}
}

// Send verschickt eine Nachricht über die aktuelle Verbindung.
func (c *Client) Send(ctx context.Context, env *Envelope) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("keine verbindung")
	}
	return writeEnvelope(ctx, conn, env)
}

func writeEnvelope(ctx context.Context, conn *websocket.Conn, env *Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

func readEnvelope(ctx context.Context, conn *websocket.Conn) (*Envelope, error) {
	_, b, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("nachricht dekodieren: %w", err)
	}
	if env.Type == "" {
		return nil, errors.New("nachricht ohne typ")
	}
	return &env, nil
}
