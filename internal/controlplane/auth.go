package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aronk11/havenry/internal/auth"
	"github.com/aronk11/havenry/internal/store"
)

// SessionTTL ist die Lebensdauer einer Browser-Sitzung.
const SessionTTL = 30 * 24 * time.Hour

const sessionCookie = "havenry_session"

// authService verwaltet Anmeldung, Sitzungen und Nutzer (ADR-0022).
type authService struct {
	store  store.Full
	logger *slog.Logger
}

func newAuthService(s store.Full, logger *slog.Logger) *authService {
	return &authService{store: s, logger: logger}
}

// EnsureInitialAdmin legt beim ersten Start einen Admin an.
//
// Kein Standardpasswort und kein offener Einrichtungsmodus: Beides wird
// vergessen und bleibt dann dauerhaft offen. Stattdessen ein zufälliges
// Passwort, genau einmal im Protokoll, mit Änderungszwang.
func (a *authService) EnsureInitialAdmin(ctx context.Context) error {
	n, err := a.store.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("nutzer zählen: %w", err)
	}
	if n > 0 {
		return nil
	}

	password, err := auth.GenerateInitialPassword()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}

	if err := a.store.CreateUser(ctx, store.User{
		ID: id, Username: "admin", PasswordHash: hash,
		Role: string(auth.RoleAdmin), CreatedAt: time.Now().UTC(),
		MustChangePassword: true,
	}); err != nil {
		return fmt.Errorf("admin anlegen: %w", err)
	}

	a.logger.Warn("═══════════════════════════════════════════════════════")
	a.logger.Warn("Erster Start: Zugang angelegt")
	a.logger.Warn("  Benutzername: admin")
	a.logger.Warn("  Passwort:     " + password)
	a.logger.Warn("Dieses Passwort wird nur einmal angezeigt und muss bei der")
	a.logger.Warn("ersten Anmeldung geändert werden.")
	a.logger.Warn("═══════════════════════════════════════════════════════")

	_ = a.store.AppendEvent(ctx, store.Event{
		At: time.Now().UTC(), Kind: "auth.initial_admin", Actor: "system",
		Summary: "Erster Zugang „admin\" angelegt",
	})
	return nil
}

// Login prüft die Anmeldedaten und erzeugt eine Sitzung.
func (a *authService) Login(ctx context.Context, username, password string) (string, store.User, error) {
	u, err := a.store.UserByName(ctx, username)
	if err != nil {
		// Auch bei unbekanntem Nutzer wird gehasht, damit die Antwortzeit
		// nicht verrät, ob es den Benutzernamen gibt.
		_ = auth.VerifyPassword(password, dummyHash)
		return "", store.User{}, auth.ErrBadPassword
	}
	if err := auth.VerifyPassword(password, u.PasswordHash); err != nil {
		return "", store.User{}, auth.ErrBadPassword
	}

	token, err := auth.NewSecret()
	if err != nil {
		return "", store.User{}, err
	}
	now := time.Now().UTC()
	if err := a.store.CreateSession(ctx, store.Session{
		TokenHash: auth.HashSecret(token), UserID: u.ID,
		CreatedAt: now, ExpiresAt: now.Add(SessionTTL),
	}); err != nil {
		return "", store.User{}, err
	}
	_ = a.store.TouchLogin(ctx, u.ID, now)

	return token, u, nil
}

// dummyHash dient dem Zeitausgleich bei unbekanntem Benutzernamen.
// Passwort ist irrelevant — es wird nie erfolgreich geprüft.
var dummyHash = mustHash("platzhalter-fuer-zeitausgleich")

func mustHash(s string) string {
	h, err := auth.HashPassword(s)
	if err != nil {
		panic(err)
	}
	return h
}

// identityFromRequest bestimmt den handelnden Nutzer.
//
// Zwei Wege: Sitzungs-Cookie (Browser) oder Bearer-Token (Automatisierung).
// Ein Token kann nie mehr dürfen als der Nutzer dahinter (ADR-0022).
func (a *authService) identityFromRequest(r *http.Request) (auth.Identity, error) {
	if hdr := r.Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer ") {
		return a.identityFromToken(r.Context(), strings.TrimPrefix(hdr, "Bearer "))
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.Identity{}, errNotAuthenticated
	}
	return a.identityFromSession(r.Context(), c.Value)
}

var errNotAuthenticated = errors.New("nicht angemeldet")

func (a *authService) identityFromSession(ctx context.Context, token string) (auth.Identity, error) {
	sess, err := a.store.SessionByHash(ctx, auth.HashSecret(token))
	if err != nil {
		return auth.Identity{}, errNotAuthenticated
	}
	u, err := a.store.UserByID(ctx, sess.UserID)
	if err != nil {
		return auth.Identity{}, errNotAuthenticated
	}
	return a.effective(ctx, u)
}

// effective löst Direktzuweisung und Teams zu einer wirksamen Identität auf
// (ADR-0029). Das passiert an genau dieser Stelle — verstreute Auflösung wäre
// die sicherste Art, die beiden Wege auseinanderlaufen zu lassen.
func (a *authService) effective(ctx context.Context, u store.User) (auth.Identity, error) {
	grants := []auth.Grant{{
		Source:  "direct",
		Role:    auth.Role(u.Role),
		HostIDs: u.HostIDs,
	}}

	teams, err := a.store.TeamsForUser(ctx, u.ID)
	if err != nil {
		// Ohne verlässliche Teamdaten wird nicht geraten. Die Direktzuweisung
		// allein zu verwenden könnte zu viel oder zu wenig erlauben; beides
		// ist schlechter als eine klare Ablehnung.
		a.logger.Error("teams eines nutzers nicht lesbar", "user", u.Username, "fehler", err)
		return auth.Identity{}, errNotAuthenticated
	}
	for _, t := range teams {
		grants = append(grants, auth.Grant{
			Source:  "team:" + t.Name,
			Role:    auth.Role(t.Role),
			HostIDs: t.HostIDs,
		})
	}
	return auth.Resolve(u.ID, u.Username, grants), nil
}

// grantsFor liefert die Rechtequellen eines Nutzers — für den Schutz des
// letzten Admins und für die Anzeige.
func (a *authService) grantsFor(ctx context.Context, u store.User) ([]auth.Grant, error) {
	grants := []auth.Grant{{Source: "direct", Role: auth.Role(u.Role), HostIDs: u.HostIDs}}
	teams, err := a.store.TeamsForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		grants = append(grants, auth.Grant{
			Source: "team:" + t.Name, Role: auth.Role(t.Role), HostIDs: t.HostIDs,
		})
	}
	return grants, nil
}

func (a *authService) identityFromToken(ctx context.Context, token string) (auth.Identity, error) {
	t, err := a.store.APITokenByHash(ctx, auth.HashSecret(token))
	if err != nil {
		return auth.Identity{}, errNotAuthenticated
	}
	u, err := a.store.UserByID(ctx, t.UserID)
	if err != nil {
		return auth.Identity{}, errNotAuthenticated
	}
	_ = a.store.TouchAPIToken(ctx, t.ID, time.Now().UTC())

	// Ein Token erbt die *wirksamen* Rechte seines Nutzers, Teams
	// eingeschlossen. Es kann damit nie mehr als der Mensch dahinter.
	id, err := a.effective(ctx, u)
	if err != nil {
		return auth.Identity{}, err
	}
	id.ViaToken = t.Name
	return id, nil
}

// identityKey ist der Kontextschlüssel für die Identität.
type identityKey struct{}

// identityFrom liefert die Identität eines Aufrufs. Nur innerhalb geschützter
// Handler aufrufen — dort ist sie garantiert vorhanden.
func identityFrom(ctx context.Context) auth.Identity {
	id, _ := ctx.Value(identityKey{}).(auth.Identity)
	return id
}

// requireAuth ist die Middleware, durch die jeder geschützte Aufruf läuft.
//
// Die Rechteprüfung passiert hier und in den Handlern (für Host-Beschränkung) —
// nicht in der Oberfläche. Die blendet Knöpfe nur zusätzlich aus; verbindlich
// ist ausschließlich der Server (ADR-0022).
func (s *Server) requireAuth(perm auth.Permission, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.auth.identityFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, errors.New("nicht angemeldet"))
			return
		}
		if !id.Can(perm) {
			s.logger.Warn("zugriff abgelehnt",
				"nutzer", id.Username, "rolle", id.Role, "benoetigt", perm, "pfad", r.URL.Path)
			writeErr(w, http.StatusForbidden,
				fmt.Errorf("rolle %q darf das nicht", id.Role))
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}
}

// requireHostAccess prüft zusätzlich die Host-Beschränkung.
//
// Getrennt von requireAuth, weil die Host-ID erst im Handler bekannt ist.
// Rückgabe false bedeutet: Antwort wurde bereits geschrieben.
func (s *Server) requireHostAccess(w http.ResponseWriter, r *http.Request, hostID string) bool {
	id := identityFrom(r.Context())
	if id.CanAccessHost(hostID) {
		return true
	}
	s.logger.Warn("host-zugriff abgelehnt", "nutzer", id.Username, "host_id", hostID)
	// Bewusst 404 statt 403: Ein Nutzer ohne Zugriff soll nicht erfahren,
	// dass es diesen Host überhaupt gibt.
	writeErr(w, http.StatusNotFound, errors.New("host nicht gefunden"))
	return false
}

// visibleHosts filtert eine Hostliste auf das, was der Nutzer sehen darf.
func visibleHosts(id auth.Identity, hosts []store.Host) []store.Host {
	if id.Role == auth.RoleAdmin || len(id.HostIDs) == 0 {
		return hosts
	}
	out := make([]store.Host, 0, len(hosts))
	for _, h := range hosts {
		if id.CanAccessHost(h.ID) {
			out = append(out, h)
		}
	}
	return out
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sessionSameSite wählt die SameSite-Regel für die Sitzungs-Cookies.
//
// Lax reicht, solange die Oberfläche aus demselben Binary kommt. Eine
// getrennt ausgelieferte Console (ADR-0032) läuft unter einer anderen
// Herkunft, und Lax-Cookies gehen bei einem Cross-Origin-fetch() gar nicht
// erst mit — Browser schicken sie nur bei einer Top-Level-Navigation, nicht
// bei einer per JavaScript ausgelösten Anfrage. Ohne None wäre die Console
// technisch nie in der Lage, sich anzumelden, egal wie CORS steht.
//
// None ohne Secure verwirft jeder aktuelle Browser ohnehin, deshalb der
// Rückfall auf Lax im TLS-losen Testbetrieb (HAVENRY_TLS=off) — dort bleibt
// nur der Zugriff über dieselbe Herkunft möglich, was für lokale Tests reicht.
// Der eigentliche Schutz gegen CSRF hängt an dieser Stelle nicht mehr an
// SameSite, sondern an der Content-Type-Prüfung in decodeBody: Ohne sie wäre
// SameSite=None die klassische Lücke für JSON-CSRF per <form>.
func sessionSameSite(secure bool) http.SameSite {
	if secure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:  sessionCookie,
		Value: token,
		Path:  "/",
		// HttpOnly: kein Zugriff aus JavaScript, damit ein XSS-Fehler nicht
		// sofort die Sitzung ausleitet.
		HttpOnly: true,
		SameSite: sessionSameSite(secure),
		Secure:   secure,
		MaxAge:   int(SessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/",
		HttpOnly: true, SameSite: sessionSameSite(secure), Secure: secure, MaxAge: -1,
	})
}
