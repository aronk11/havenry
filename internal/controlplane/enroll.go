// Package controlplane enthält die Orchestrierung: Enrollment, API, Auth.
package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aronk11/havenry/internal/store"
	"github.com/aronk11/havenry/internal/transport"
)

// DefaultEnrollTTL ist die Lebensdauer eines Enrollment-Tokens (ADR-0015).
const DefaultEnrollTTL = 15 * time.Minute

// Enroller setzt ADR-0015 um: Token ausgeben, gegen ein dauerhaftes Credential
// tauschen, Hosts vor der ersten Kommandoausführung bestätigen lassen.
type Enroller struct {
	store  store.Store
	logger *slog.Logger
	ttl    time.Duration
	now    func() time.Time
}

func NewEnroller(s store.Store, logger *slog.Logger) *Enroller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Enroller{store: s, logger: logger, ttl: DefaultEnrollTTL, now: time.Now}
}

// IssueToken erzeugt ein einmaliges Enrollment-Token.
//
// Das Klartext-Token wird nur hier zurückgegeben und nirgends gespeichert —
// die Datenbank hält ausschließlich den Hash. Wer die Datenbank liest, kann
// damit keinen Host anhängen.
//
// actor ist der Nutzername für das Ereignisprotokoll. Ein Protokoll, das nur
// "user" vermerkt, taugt nicht als Nachweis (ADR-0022).
func (e *Enroller) IssueToken(ctx context.Context, actor string) (string, time.Time, error) {
	tok, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := e.now()
	expires := now.Add(e.ttl)

	if err := e.store.CreateEnrollToken(ctx, store.EnrollToken{
		TokenHash: hashToken(tok),
		CreatedAt: now,
		ExpiresAt: expires,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("token ablegen: %w", err)
	}

	_ = e.store.AppendEvent(ctx, store.Event{
		At: now, Kind: "enroll.token_issued", Actor: actor,
		Summary: "Enrollment-Token ausgestellt",
	})
	return tok, expires, nil
}

// Authenticate implementiert transport.Authenticator.
//
// Zwei Wege:
//   - Credential vorhanden → wiederkehrender Agent
//   - Enrollment-Token vorhanden → Erstverbindung, Token wird eingelöst
func (e *Enroller) Authenticate(ctx context.Context, h transport.Hello) (transport.HelloAck, error) {
	switch {
	case h.Credential != "":
		return e.authenticateExisting(ctx, h)
	case h.EnrollToken != "":
		return e.enrollNew(ctx, h)
	default:
		return transport.HelloAck{}, &transport.AuthError{
			Code:    transport.ErrBadCredential,
			Message: "weder credential noch enrollment-token angegeben",
		}
	}
}

func (e *Enroller) authenticateExisting(ctx context.Context, h transport.Hello) (transport.HelloAck, error) {
	hash := hashToken(h.Credential)

	host, err := e.store.HostByCredentialHash(ctx, hash)
	if err != nil {
		// Bewusst dieselbe Meldung wie bei einem falschen Credential:
		// kein Rückschluss darauf, ob ein Host existiert.
		return transport.HelloAck{}, &transport.AuthError{
			Code:    transport.ErrBadCredential,
			Message: "credential unbekannt — host neu enrollen",
		}
	}
	// Konstantzeit-Vergleich, obwohl der Lookup schon getroffen hat:
	// schützt gegen künftige Umbauten auf einen listenbasierten Lookup.
	if subtle.ConstantTimeCompare([]byte(host.CredentialHash), []byte(hash)) != 1 {
		return transport.HelloAck{}, &transport.AuthError{
			Code:    transport.ErrBadCredential,
			Message: "credential unbekannt — host neu enrollen",
		}
	}

	now := e.now()
	host.Hostname = h.Hostname
	host.AgentVersion = h.AgentVersion
	host.OS, host.Arch = h.OS, h.Arch
	host.LastSeen = now
	if err := e.store.UpsertHost(ctx, host); err != nil {
		return transport.HelloAck{}, err
	}

	return transport.HelloAck{HostID: host.ID, Approved: host.Approved}, nil
}

func (e *Enroller) enrollNew(ctx context.Context, h transport.Hello) (transport.HelloAck, error) {
	now := e.now()

	// Prüfen und Entwerten in einem Schritt — ein Token darf nur einmal wirken.
	if err := e.store.ConsumeEnrollToken(ctx, hashToken(h.EnrollToken), now); err != nil {
		switch {
		case errors.Is(err, store.ErrTokenExpired):
			return transport.HelloAck{}, &transport.AuthError{
				Code:    transport.ErrTokenExpired,
				Message: "enrollment-token abgelaufen — neues token in der oberfläche erzeugen",
			}
		case errors.Is(err, store.ErrTokenUsed):
			return transport.HelloAck{}, &transport.AuthError{
				Code:    transport.ErrTokenExpired,
				Message: "enrollment-token wurde bereits verwendet",
			}
		default:
			return transport.HelloAck{}, &transport.AuthError{
				Code:    transport.ErrBadCredential,
				Message: "enrollment-token ungültig",
			}
		}
	}

	hostID, err := randomToken(12)
	if err != nil {
		return transport.HelloAck{}, err
	}
	cred, err := randomToken(32)
	if err != nil {
		return transport.HelloAck{}, err
	}

	host := store.Host{
		ID:             hostID,
		Hostname:       h.Hostname,
		CredentialHash: hashToken(cred),
		// Bewusst false: ein geleaktes Token allein reicht nicht aus, um einen
		// fremden Host handlungsfähig anzuhängen. Bestätigung erfolgt in der UI.
		Approved:     false,
		OS:           h.OS,
		Arch:         h.Arch,
		AgentVersion: h.AgentVersion,
		EnrolledAt:   now,
		LastSeen:     now,
	}
	if err := e.store.UpsertHost(ctx, host); err != nil {
		return transport.HelloAck{}, err
	}

	_ = e.store.AppendEvent(ctx, store.Event{
		At: now, HostID: hostID, Kind: "enroll.host_added", Actor: "agent",
		Summary: fmt.Sprintf("Host %q enrollt, wartet auf Bestätigung", h.Hostname),
		Details: map[string]string{"os": h.OS, "arch": h.Arch, "agent_version": h.AgentVersion},
	})

	e.logger.Info("host enrollt — bestätigung ausstehend", "host_id", hostID, "hostname", h.Hostname)

	// Credential wird genau einmal übertragen, danach nie wieder.
	return transport.HelloAck{HostID: hostID, Credential: cred, Approved: false}, nil
}

// Approve bestätigt einen Host. Erst danach führt er Kommandos aus.
//
// Das Ereignis wird hier geschrieben und nicht im Handler — sonst entstehen
// zwei Einträge für einen Vorgang, was ein Protokoll unbrauchbar macht.
func (e *Enroller) Approve(ctx context.Context, hostID, actor string) error {
	host, err := e.store.HostByID(ctx, hostID)
	if err != nil {
		return err
	}
	if err := e.store.ApproveHost(ctx, hostID); err != nil {
		return err
	}
	_ = e.store.AppendEvent(ctx, store.Event{
		At: e.now(), HostID: hostID, Kind: "hosts.approved", Actor: actor,
		Summary: fmt.Sprintf("Host %q bestätigt", host.Hostname),
	})
	return nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("zufall erzeugen: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken hasht Tokens und Credentials vor der Ablage.
//
// SHA-256 ohne Salt ist hier korrekt und kein Versehen: Es handelt sich um
// 256-Bit-Zufallswerte, nicht um Passwörter. Brute-Force ist aussichtslos,
// Rainbow-Tables sind ohne rateraum-armen Eingaberaum wirkungslos. Für
// Nutzerpasswörter wird später Argon2id verwendet.
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

var _ transport.Authenticator = (*Enroller)(nil)
