// Package store kapselt die Persistenz der Control Plane.
//
// Der Inhalt ist ausschließlich abgeleiteter Zustand (ADR-0002): Ist-Zustand
// der Hosts, Events, Metrik-Cache, Credentials. Die Datenbank ist jederzeit
// löschbar — Hosts müssen sich dann neu enrollen, die Stacks kommen aus Git.
package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("nicht gefunden")

// Host ist ein registrierter Agent-Host.
type Host struct {
	ID       string
	Hostname string
	// CredentialHash speichert nie das Credential selbst.
	CredentialHash string
	// Approved wird durch Bestätigung in der Oberfläche gesetzt (ADR-0015).
	Approved     bool
	OS           string
	Arch         string
	AgentVersion string
	EnrolledAt   time.Time
	LastSeen     time.Time
}

// EnrollToken ist ein einmaliges, kurzlebiges Token für die Erstverbindung.
type EnrollToken struct {
	// TokenHash — das Klartext-Token existiert nur einmal in der Oberfläche.
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// Event protokolliert jede Aktion der Plattform mit Auslöser (ADR-0018).
//
// Die JSON-Tags sind Teil des API-Vertrags (ADR-0009) — Go-Feldnamen dürfen
// nicht nach außen durchschlagen.
type Event struct {
	ID      int64             `json:"id"`
	At      time.Time         `json:"at"`
	HostID  string            `json:"host_id,omitempty"`
	Kind    string            `json:"kind"`
	Actor   string            `json:"actor"` // "system" | "user" | "agent"
	Summary string            `json:"summary"`
	Details map[string]string `json:"details,omitempty"`
}

// Store ist die Persistenzschnittstelle. Die SQLite-Implementierung folgt in M2;
// bis dahin dient die In-Memory-Variante zum Bau und Test von M1.
type Store interface {
	CreateEnrollToken(ctx context.Context, t EnrollToken) error
	// ConsumeEnrollToken prüft und entwertet ein Token in einem Schritt.
	// Muss atomar sein, damit ein Token nicht mehrfach eingelöst werden kann.
	ConsumeEnrollToken(ctx context.Context, tokenHash string, now time.Time) error

	UpsertHost(ctx context.Context, h Host) error
	HostByID(ctx context.Context, id string) (Host, error)
	HostByCredentialHash(ctx context.Context, hash string) (Host, error)
	Hosts(ctx context.Context) ([]Host, error)
	ApproveHost(ctx context.Context, id string) error
	TouchHost(ctx context.Context, id string, at time.Time) error

	AppendEvent(ctx context.Context, e Event) error
	Events(ctx context.Context, limit int) ([]Event, error)
}

// User ist ein lokaler Nutzerzugang (ADR-0022).
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	// HostIDs beschränkt den Zugriff. Leer bedeutet: alle Hosts.
	HostIDs   []string
	CreatedAt time.Time
	LastLogin *time.Time
	// MustChangePassword markiert das erzeugte Startpasswort.
	MustChangePassword bool
}

// Session ist eine angemeldete Browser-Sitzung.
type Session struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// APIToken erlaubt Automatisierung mit den Rechten eines Nutzers.
type APIToken struct {
	ID        string
	TokenHash string
	UserID    string
	Name      string
	CreatedAt time.Time
	ExpiresAt *time.Time
	LastUsed  *time.Time
}

// GitRepo ist die konfigurierte Quelle des Soll-Zustands (ADR-0002).
// Es gibt genau eine — mehrere Repos wären ein Feature ohne belegten Bedarf.
type GitRepo struct {
	URL    string
	Branch string
	// BasePath erlaubt ein Unterverzeichnis in einem Monorepo.
	BasePath string
	// SSHKeyPath verweist auf einen Deploy-Key. Der Schlüssel selbst wird
	// nie in der Datenbank gespeichert (ADR-0006).
	SSHKeyPath   string
	LastSync     *time.Time
	LastCommit   string
	LastError    string
	ConfiguredAt time.Time
}

// UserStore ergänzt Store um Nutzerverwaltung.
type UserStore interface {
	CreateUser(ctx context.Context, u User) error
	UpdateUser(ctx context.Context, u User) error
	DeleteUser(ctx context.Context, id string) error
	UserByID(ctx context.Context, id string) (User, error)
	UserByName(ctx context.Context, name string) (User, error)
	Users(ctx context.Context) ([]User, error)
	CountUsers(ctx context.Context) (int, error)
	TouchLogin(ctx context.Context, id string, at time.Time) error

	CreateSession(ctx context.Context, s Session) error
	SessionByHash(ctx context.Context, hash string) (Session, error)
	DeleteSession(ctx context.Context, hash string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	PurgeExpiredSessions(ctx context.Context, now time.Time) error

	CreateAPIToken(ctx context.Context, t APIToken) error
	APITokenByHash(ctx context.Context, hash string) (APIToken, error)
	APITokensByUser(ctx context.Context, userID string) ([]APIToken, error)
	DeleteAPIToken(ctx context.Context, id string) error
	TouchAPIToken(ctx context.Context, id string, at time.Time) error
}

// RepoStore ergänzt Store um die Git-Konfiguration.
type RepoStore interface {
	SaveRepo(ctx context.Context, r GitRepo) error
	Repo(ctx context.Context) (GitRepo, error)
	ClearRepo(ctx context.Context) error
}

// LocalStack ist ein Compose-Stack, den Havenry selbst verwaltet statt Git
// (ADR-0034).
//
// Bewusste Ausnahme von der package-Doc oben: Für lokale Stacks ist die
// Datenbank nicht abgeleiteter Zustand, sondern die einzige Kopie. Wer keinen
// Git-Workflow will, bekommt hier die Bequemlichkeit — und trägt dafür die
// Verantwortung fürs Backup, die sonst Git übernimmt.
type LocalStack struct {
	ID          string
	HostID      string
	Name        string
	ComposeYAML string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// UpdatedBy ist der Benutzername des letzten Bearbeiters (ADR-0018).
	UpdatedBy string
}

// LocalStackStore ergänzt Store um in Havenry selbst gepflegte Stacks.
type LocalStackStore interface {
	CreateLocalStack(ctx context.Context, st LocalStack) error
	UpdateLocalStack(ctx context.Context, st LocalStack) error
	DeleteLocalStack(ctx context.Context, hostID, name string) error
	LocalStackByName(ctx context.Context, hostID, name string) (LocalStack, error)
	LocalStacksForHost(ctx context.Context, hostID string) ([]LocalStack, error)
	LocalStacks(ctx context.Context) ([]LocalStack, error)
}

// Full ist die vollständige Persistenzschnittstelle.
type Full interface {
	Store
	UserStore
	RepoStore
	TeamStore
	LocalStackStore
}

// Team bündelt eine Rolle und eine Host-Menge (ADR-0029).
//
// Teams ergänzen die Direktzuweisung am Nutzer, sie ersetzen sie nicht. Die
// wirksamen Rechte sind die Vereinigung aus beidem.
type Team struct {
	ID          string
	Name        string
	Description string
	Role        string
	// HostIDs beschränkt den Zugriff des Teams. Leer bedeutet: alle Hosts.
	HostIDs   []string
	CreatedAt time.Time
}

// TeamMember ist eine Mitgliedschaft.
type TeamMember struct {
	TeamID   string
	UserID   string
	JoinedAt time.Time
}

// TeamStore ergänzt Store um Teams und Mitgliedschaften.
type TeamStore interface {
	CreateTeam(ctx context.Context, t Team) error
	UpdateTeam(ctx context.Context, t Team) error
	DeleteTeam(ctx context.Context, id string) error
	TeamByID(ctx context.Context, id string) (Team, error)
	Teams(ctx context.Context) ([]Team, error)

	AddTeamMember(ctx context.Context, teamID, userID string, at time.Time) error
	RemoveTeamMember(ctx context.Context, teamID, userID string) error
	TeamMembers(ctx context.Context, teamID string) ([]User, error)
	// TeamsForUser liefert alle Teams eines Nutzers — die Grundlage für die
	// Auflösung der wirksamen Rechte.
	TeamsForUser(ctx context.Context, userID string) ([]Team, error)
}
