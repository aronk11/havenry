package controlplane

import (
	"net/http"

	"github.com/aronk11/havenry/internal/auth"
)

// APIVersion ist die Version dieses Baums.
//
// Eine Version ist ein eigener Baum, keine Einstellung (ADR-0030). `v2` wird
// eines Tages neben `v1` gemountet, und beide laufen gleichzeitig. Deshalb
// liegt die Registrierung in einer Funktion pro Version, statt dass Pfade
// verstreut im Router entstehen — ohne diese Trennung ist ein zweiter Baum
// später nicht sauber einzuziehen.
const APIVersion = "v1"

// APIVersionInfo beschreibt eine Version für /api/versions.
type APIVersionInfo struct {
	Version string `json:"version"`
	Prefix  string `json:"prefix"`
	// Deprecated meldet, dass diese Version zurückgezogen wird. Ein Client
	// kann darauf reagieren, statt eines Tages an einem 404 zu raten.
	Deprecated bool `json:"deprecated"`
	// SunsetAfter ist gesetzt, sobald ein Rückzugsdatum feststeht.
	SunsetAfter string `json:"sunset_after,omitempty"`
}

// SupportedAPIVersions ist die Antwort von /api/versions.
var SupportedAPIVersions = []APIVersionInfo{
	{Version: "v1", Prefix: "/api/v1", Deprecated: false},
}

// registerV1 hängt alle v1-Routen an den Router.
//
// Jede Route außer der Anmeldung läuft durch requireAuth mit der nötigen
// Fähigkeit. Die Host-Beschränkung wird zusätzlich im Handler geprüft, weil
// die Host-ID erst dort bekannt ist (ADR-0022).
func registerV1(mux *http.ServeMux, s *Server) {
	const p = "/api/v1"

	// --- Anmeldung: zwangsläufig offen ---
	mux.HandleFunc("POST "+p+"/auth/login", s.login)
	mux.HandleFunc("POST "+p+"/auth/logout", s.logout)

	// --- Eigenes Konto: jede angemeldete Rolle ---
	mux.HandleFunc("GET "+p+"/auth/me", s.requireAuth(auth.PermViewHosts, s.me))
	mux.HandleFunc("POST "+p+"/auth/password", s.requireAuth(auth.PermViewHosts, s.changeOwnPassword))
	mux.HandleFunc("GET "+p+"/auth/tokens", s.requireAuth(auth.PermViewHosts, s.listAPITokens))
	mux.HandleFunc("POST "+p+"/auth/tokens", s.requireAuth(auth.PermViewHosts, s.createAPIToken))
	mux.HandleFunc("DELETE "+p+"/auth/tokens/{id}", s.requireAuth(auth.PermViewHosts, s.deleteAPIToken))

	// --- Nutzer: nur admin ---
	mux.HandleFunc("GET "+p+"/users", s.requireAuth(auth.PermManageUsers, s.listUsers))
	mux.HandleFunc("POST "+p+"/users", s.requireAuth(auth.PermManageUsers, s.createUser))
	mux.HandleFunc("PATCH "+p+"/users/{id}", s.requireAuth(auth.PermManageUsers, s.updateUser))
	mux.HandleFunc("DELETE "+p+"/users/{id}", s.requireAuth(auth.PermManageUsers, s.deleteUser))

	// --- Teams: nur admin (ADR-0029) ---
	mux.HandleFunc("GET "+p+"/teams", s.requireAuth(auth.PermManageUsers, s.listTeams))
	mux.HandleFunc("POST "+p+"/teams", s.requireAuth(auth.PermManageUsers, s.createTeam))
	mux.HandleFunc("GET "+p+"/teams/{id}", s.requireAuth(auth.PermManageUsers, s.getTeam))
	mux.HandleFunc("PATCH "+p+"/teams/{id}", s.requireAuth(auth.PermManageUsers, s.updateTeam))
	mux.HandleFunc("DELETE "+p+"/teams/{id}", s.requireAuth(auth.PermManageUsers, s.deleteTeam))
	mux.HandleFunc("PUT "+p+"/teams/{id}/members/{userID}", s.requireAuth(auth.PermManageUsers, s.addTeamMember))
	mux.HandleFunc("DELETE "+p+"/teams/{id}/members/{userID}", s.requireAuth(auth.PermManageUsers, s.removeTeamMember))

	// --- Hosts und Zustand ---
	mux.HandleFunc("GET "+p+"/hosts", s.requireAuth(auth.PermViewHosts, s.listHosts))
	mux.HandleFunc("POST "+p+"/hosts/{id}/approve", s.requireAuth(auth.PermApproveHost, s.approveHost))
	mux.HandleFunc("POST "+p+"/enroll-tokens", s.requireAuth(auth.PermApproveHost, s.issueToken))
	mux.HandleFunc("GET "+p+"/events", s.requireAuth(auth.PermViewHosts, s.listEvents))
	mux.HandleFunc("GET "+p+"/stacks", s.requireAuth(auth.PermViewHosts, s.listStacks))
	mux.HandleFunc("GET "+p+"/containers", s.requireAuth(auth.PermViewHosts, s.listContainers))

	// --- Steuern und Logs ---
	mux.HandleFunc("POST "+p+"/containers/{hostID}/{id}/{action}",
		s.requireAuth(auth.PermControlDocker, s.containerAction))
	mux.HandleFunc("GET "+p+"/containers/{hostID}/{id}/logs",
		s.requireAuth(auth.PermViewLogs, s.containerLogs))

	// --- Abweichungen ---
	mux.HandleFunc("GET "+p+"/drift", s.requireAuth(auth.PermViewHosts, s.listDrift))
	mux.HandleFunc("POST "+p+"/drift/{hostID}/{stack}/{action}",
		s.requireAuth(auth.PermAdoptRevert, s.resolveDrift))

	// --- Lokale Stacks (ADR-0034): Compose-Definitionen ohne Git ---
	mux.HandleFunc("GET "+p+"/hosts/{hostID}/local-stacks",
		s.requireAuth(auth.PermViewHosts, s.listLocalStacks))
	mux.HandleFunc("POST "+p+"/hosts/{hostID}/local-stacks",
		s.requireAuth(auth.PermManageStacks, s.createLocalStack))
	mux.HandleFunc("GET "+p+"/hosts/{hostID}/local-stacks/{name}",
		s.requireAuth(auth.PermViewHosts, s.getLocalStack))
	mux.HandleFunc("PUT "+p+"/hosts/{hostID}/local-stacks/{name}",
		s.requireAuth(auth.PermManageStacks, s.updateLocalStack))
	mux.HandleFunc("DELETE "+p+"/hosts/{hostID}/local-stacks/{name}",
		s.requireAuth(auth.PermManageStacks, s.deleteLocalStack))
	mux.HandleFunc("POST "+p+"/hosts/{hostID}/local-stacks/{name}/apply",
		s.requireAuth(auth.PermAdoptRevert, s.applyLocalStack))

	// --- Repository ---
	mux.HandleFunc("GET "+p+"/repo", s.requireAuth(auth.PermViewHosts, s.getRepo))
	mux.HandleFunc("PUT "+p+"/repo", s.requireAuth(auth.PermManageRepo, s.setRepo))
	mux.HandleFunc("POST "+p+"/repo/sync", s.requireAuth(auth.PermManageRepo, s.syncRepo))
	mux.HandleFunc("DELETE "+p+"/repo", s.requireAuth(auth.PermManageRepo, s.deleteRepo))
}

// withAPIVersionHeader vermerkt in jeder Antwort, welcher Baum geantwortet hat.
// In Protokollen und beim Debuggen ist das die erste Frage.
func withAPIVersionHeader(version string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Havenry-API-Version", version)
		next.ServeHTTP(w, r)
	})
}

// listAPIVersions ist bewusst unversioniert und offen.
//
// Ein Client muss beim Start herausfinden können, ob er noch bedient wird —
// und zwar bevor er sich anmeldet. Eine versionierte Versionsauskunft wäre ein
// Henne-Ei-Problem.
func (s *Server) listAPIVersions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"versions": SupportedAPIVersions,
		"current":  APIVersion,
		"server":   s.version,
	})
}
