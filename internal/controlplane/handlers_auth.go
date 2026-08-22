package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aronk11/havenry/internal/auth"
	"github.com/aronk11/havenry/internal/store"
)

type userView struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Role               string     `json:"role"`
	HostIDs            []string   `json:"host_ids"`
	CreatedAt          time.Time  `json:"created_at"`
	LastLogin          *time.Time `json:"last_login,omitempty"`
	MustChangePassword bool       `json:"must_change_password"`
}

func toUserView(u store.User) userView {
	ids := u.HostIDs
	if ids == nil {
		ids = []string{}
	}
	return userView{
		ID: u.ID, Username: u.Username, Role: u.Role, HostIDs: ids,
		CreatedAt: u.CreatedAt, LastLogin: u.LastLogin,
		MustChangePassword: u.MustChangePassword,
	}
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()

	// Ohne diese Prüfung ließe sich mit SameSite=None (ADR-0032, für eine
	// getrennt ausgelieferte Console) die klassische JSON-CSRF über ein
	// <form enctype="text/plain"> fahren: Ein Browser darf bei einem
	// "einfachen" Formular-Request Content-Type nur auf
	// application/x-www-form-urlencoded, multipart/form-data oder
	// text/plain setzen — nie auf application/json. Wer also exakt
	// application/json verlangt, schließt den Vektor unabhängig von
	// SameSite. Ohne Cross-Origin-Console wäre SameSite=Lax allein bereits
	// ausreichend gewesen; mit ihr ist das hier keine Kür mehr.
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("anfrage unlesbar: Content-Type muss application/json sein, war %q", ct)
	}

	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("anfrage unlesbar: %w", err)
	}
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// Nach Benutzername und Quelladresse getrennt bremsen.
	userKey, ipKey := "u:"+strings.ToLower(body.Username), "ip:"+clientIP(r)
	if ok, wait := s.limiter.Allowed(userKey, ipKey); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())))
		writeErr(w, http.StatusTooManyRequests,
			fmt.Errorf("zu viele fehlversuche — bitte %s warten", wait))
		return
	}

	token, u, err := s.auth.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		s.limiter.Fail(userKey, ipKey)
		// Fehlversuche werden protokolliert — bei einem Werkzeug mit
		// Root-Rechten auf allen Hosts gehört das zum Nachweis (ADR-0018).
		_ = s.store.AppendEvent(r.Context(), store.Event{
			At: time.Now().UTC(), Kind: "auth.login_failed", Actor: "unbekannt",
			Summary: fmt.Sprintf("Fehlgeschlagene Anmeldung für %q", body.Username),
			Details: map[string]string{"remote": clientIP(r)},
		})
		writeErr(w, http.StatusUnauthorized, auth.ErrBadPassword)
		return
	}

	s.limiter.Succeed(userKey, ipKey)
	setSessionCookie(w, token, r.TLS != nil)
	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "auth.login", Actor: u.Username,
		Summary: "Angemeldet",
		Details: map[string]string{"remote": clientIP(r)},
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserView(u)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), auth.HashSecret(c.Value))
	}
	clearSessionCookie(w, r.TLS != nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "abgemeldet"})
}

// me liefert die *wirksame* Identität, nicht den gespeicherten Datensatz.
//
// Das ist seit ADR-0029 ein Unterschied mit Folgen: Wer als viewer angelegt
// ist und über ein Team operator-Rechte hat, sieht sonst "viewer" und hält die
// Oberfläche für kaputt, wenn Knöpfe erscheinen, die er laut Anzeige nicht
// haben dürfte. Umgekehrt wäre es noch schlimmer — jemand hielte sich für
// harmloser, als er ist.
//
// Die Direktzuweisung wird zusätzlich mitgeliefert, damit ein Admin sehen
// kann, woher was kommt.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	u, err := s.store.UserByID(r.Context(), id.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	perms := []string{}
	for _, p := range []auth.Permission{
		auth.PermViewHosts, auth.PermApproveHost, auth.PermControlDocker,
		auth.PermViewLogs, auth.PermManageUsers, auth.PermManageRepo, auth.PermAdoptRevert,
	} {
		if id.Can(p) {
			perms = append(perms, string(p))
		}
	}

	teams, _ := s.store.TeamsForUser(r.Context(), u.ID)
	teamNames := make([]string, 0, len(teams))
	for _, t := range teams {
		teamNames = append(teamNames, t.Name)
	}

	hostIDs := id.HostIDs
	if hostIDs == nil {
		hostIDs = []string{}
	}
	sources := id.Sources
	if sources == nil {
		sources = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		// Wirksam — das ist, was zählt.
		"user": map[string]any{
			"id":                   u.ID,
			"username":             u.Username,
			"role":                 string(id.Role),
			"host_ids":             hostIDs,
			"must_change_password": u.MustChangePassword,
			"created_at":           u.CreatedAt,
			"last_login":           u.LastLogin,
		},
		"permissions": perms,
		"teams":       teamNames,
		// Woher die Rechte stammen, und was direkt am Konto steht.
		"granted_by": sources,
		"direct": map[string]any{
			"role":     u.Role,
			"host_ids": u.HostIDs,
		},
	})
}

// changeOwnPassword ändert das eigene Passwort.
func (s *Server) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	id := identityFrom(r.Context())
	u, err := s.store.UserByID(r.Context(), id.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err := auth.VerifyPassword(body.Current, u.PasswordHash); err != nil {
		writeErr(w, http.StatusForbidden, errors.New("aktuelles passwort falsch"))
		return
	}
	hash, err := auth.HashPassword(body.New)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	u.PasswordHash = hash
	u.MustChangePassword = false
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Alle anderen Sitzungen beenden und eine frische ausstellen: Wer das
	// Passwort ändert, tut das oft, weil er einen Zugriff vermutet.
	_ = s.store.DeleteUserSessions(r.Context(), u.ID)
	token, _, err := s.auth.Login(r.Context(), u.Username, body.New)
	if err == nil {
		setSessionCookie(w, token, r.TLS != nil)
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "auth.password_changed", Actor: u.Username,
		Summary: "Eigenes Passwort geändert",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "geändert"})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.Users(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]userView, 0, len(users))
	for _, u := range users {
		out = append(out, toUserView(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Role     string   `json:"role"`
		HostIDs  []string `json:"host_ids"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := auth.ValidateUsername(body.Username); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	role := auth.Role(body.Role)
	if !role.Valid() {
		writeErr(w, http.StatusBadRequest,
			fmt.Errorf("rolle %q unbekannt (erlaubt: admin, operator, viewer)", body.Role))
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := newID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	u := store.User{
		ID: id, Username: body.Username, PasswordHash: hash,
		Role: string(role), HostIDs: body.HostIDs,
		CreatedAt: time.Now().UTC(), MustChangePassword: true,
	}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		if errors.Is(err, store.ErrUserExists) {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	actor := identityFrom(r.Context())
	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "users.created", Actor: actor.Actor(),
		Summary: fmt.Sprintf("Nutzer %q angelegt (%s)", u.Username, u.Role),
		Details: map[string]string{"hosts": fmt.Sprint(len(u.HostIDs))},
	})
	writeJSON(w, http.StatusCreated, map[string]any{"user": toUserView(u)})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	var body struct {
		Role     *string   `json:"role"`
		HostIDs  *[]string `json:"host_ids"`
		Password *string   `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	u, err := s.store.UserByID(r.Context(), targetID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	actor := identityFrom(r.Context())

	if body.Role != nil {
		role := auth.Role(*body.Role)
		if !role.Valid() {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("rolle %q unbekannt", *body.Role))
			return
		}
		// Niemand darf sich so herabstufen, dass danach kein Admin mehr übrig
		// ist — die Installation wäre nicht mehr verwaltbar. Die Rechnung
		// berücksichtigt Teams (ADR-0029).
		if u.Role == string(auth.RoleAdmin) && role != auth.RoleAdmin {
			if s.wouldStrandInstallation(r, excludeDirectForUser(u.ID)) {
				writeErr(w, http.StatusConflict, errors.New(
					"danach hätte niemand mehr Adminrechte — vorher einen weiteren Admin einrichten"))
				return
			}
		}
		u.Role = string(role)
	}
	if body.HostIDs != nil {
		u.HostIDs = *body.HostIDs
	}
	if body.Password != nil {
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		u.PasswordHash = hash
		u.MustChangePassword = true
	}

	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Rollen- oder Rechteänderung beendet bestehende Sitzungen: Eine alte
	// Sitzung darf keine alten Rechte weitertragen.
	_ = s.store.DeleteUserSessions(r.Context(), u.ID)

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "users.updated", Actor: actor.Actor(),
		Summary: fmt.Sprintf("Nutzer %q geändert", u.Username),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserView(u)})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	actor := identityFrom(r.Context())

	if targetID == actor.UserID {
		writeErr(w, http.StatusConflict, errors.New("man kann sich nicht selbst löschen"))
		return
	}
	u, err := s.store.UserByID(r.Context(), targetID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if s.wouldStrandInstallation(r, excludeUser(u.ID)) {
		writeErr(w, http.StatusConflict, errors.New(
			"danach hätte niemand mehr Adminrechte"))
		return
	}
	if err := s.store.DeleteUser(r.Context(), targetID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "users.deleted", Actor: actor.Actor(),
		Summary: fmt.Sprintf("Nutzer %q gelöscht", u.Username),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	tokens, err := s.store.APITokensByUser(r.Context(), id.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	type view struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		LastUsed  *time.Time `json:"last_used,omitempty"`
	}
	out := make([]view, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, view{t.ID, t.Name, t.CreatedAt, t.ExpiresAt, t.LastUsed})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		ExpireIn string `json:"expire_in"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name ist erforderlich"))
		return
	}

	id := identityFrom(r.Context())
	secret, err := auth.NewSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	tokenID, err := newID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	t := store.APIToken{
		ID: tokenID, TokenHash: auth.HashSecret(secret), UserID: id.UserID,
		Name: body.Name, CreatedAt: time.Now().UTC(),
	}
	if body.ExpireIn != "" {
		d, err := time.ParseDuration(body.ExpireIn)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("expire_in unlesbar: %w", err))
			return
		}
		exp := time.Now().UTC().Add(d)
		t.ExpiresAt = &exp
	}
	if err := s.store.CreateAPIToken(r.Context(), t); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), Kind: "auth.token_created", Actor: id.Actor(),
		Summary: fmt.Sprintf("API-Token %q erstellt", body.Name),
	})
	// Das Geheimnis erscheint genau einmal — hier.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": t.ID, "name": t.Name, "token": secret,
		"expires_at": t.ExpiresAt,
		"hinweis":    "Token wird nur einmal angezeigt. Verwendung: Authorization: Bearer <token>",
	})
}

func (s *Server) deleteAPIToken(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	tokenID := r.PathValue("id")

	// Nur eigene Token dürfen gelöscht werden — auch ein Admin löscht nicht
	// versehentlich fremde Automatisierung.
	tokens, err := s.store.APITokensByUser(r.Context(), id.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	found := false
	for _, t := range tokens {
		if t.ID == tokenID {
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, errors.New("token nicht gefunden"))
		return
	}
	if err := s.store.DeleteAPIToken(r.Context(), tokenID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

// clientIP liefert die Gegenstelle für das Protokoll.
//
// X-Forwarded-For wird bewusst NICHT ausgewertet: Der Wert ist frei setzbar,
// solange nicht feststeht, dass ein vertrauenswürdiger Proxy davorsteht. Eine
// fälschbare Adresse im Protokoll ist schlechter als die echte Gegenstelle.
func clientIP(r *http.Request) string {
	return r.RemoteAddr
}
