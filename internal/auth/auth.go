// Package auth setzt ADR-0022 um: lokale Nutzer, drei Rollen,
// Host-Beschränkung und die Rechteprüfung, die bei jedem Aufruf greift.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Role bestimmt, was ein Nutzer grundsätzlich darf.
type Role string

const (
	// RoleAdmin darf alles, einschließlich Nutzerverwaltung und Host-Bestätigung.
	RoleAdmin Role = "admin"
	// RoleOperator darf Container steuern und Logs lesen — auf erlaubten Hosts.
	RoleOperator Role = "operator"
	// RoleViewer darf ausschließlich lesen — auf erlaubten Hosts.
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

// Permission benennt eine konkrete Fähigkeit.
type Permission string

const (
	PermViewHosts     Permission = "hosts.view"
	PermApproveHost   Permission = "hosts.approve"
	PermControlDocker Permission = "containers.control"
	PermViewLogs      Permission = "containers.logs"
	PermManageUsers   Permission = "users.manage"
	PermManageRepo    Permission = "repo.manage"
	PermAdoptRevert   Permission = "drift.resolve"
	// PermManageStacks steuert lokal in Havenry gepflegte Stack-Definitionen
	// (ADR-0034) — bewusst auf derselben Stufe wie PermManageRepo, weil beide
	// den Soll-Zustand festlegen statt nur bestehende Container zu steuern.
	PermManageStacks Permission = "stacks.manage"
)

// rolePermissions bildet Rollen auf Fähigkeiten ab.
//
// Bewusst als Tabelle und nicht als Verzweigungen im Code: Wer wissen will,
// was eine Rolle darf, liest eine Stelle statt zwanzig Handler.
var rolePermissions = map[Role]map[Permission]bool{
	RoleAdmin: {
		PermViewHosts: true, PermApproveHost: true, PermControlDocker: true,
		PermViewLogs: true, PermManageUsers: true, PermManageRepo: true,
		PermAdoptRevert: true, PermManageStacks: true,
	},
	RoleOperator: {
		PermViewHosts: true, PermControlDocker: true, PermViewLogs: true,
		PermAdoptRevert: true,
	},
	RoleViewer: {
		PermViewHosts: true, PermViewLogs: true,
	},
}

// Identity ist der handelnde Nutzer eines Aufrufs.
type Identity struct {
	UserID   string
	Username string
	Role     Role
	// HostIDs beschränkt den Zugriff. Leer bedeutet: alle Hosts.
	// Bei RoleAdmin wird die Beschränkung ignoriert.
	HostIDs []string
	// ViaToken vermerkt, dass der Aufruf über ein API-Token kam.
	// Steht im Ereignisprotokoll, damit Automatisierung von Handarbeit
	// unterscheidbar bleibt.
	ViaToken string
	// Sources nennt, woher die Rechte stammen: "direct" und/oder Teamnamen
	// (ADR-0029). Ohne diese Angabe lässt sich im Nachhinein nicht mehr
	// klären, warum jemand etwas durfte.
	Sources []string
}

// Can prüft eine Fähigkeit.
func (i Identity) Can(p Permission) bool {
	perms, ok := rolePermissions[i.Role]
	if !ok {
		return false
	}
	return perms[p]
}

// CanAccessHost prüft die Host-Beschränkung.
//
// Getrennt von Can, weil beide Fragen zusammen gestellt werden müssen: Ein
// Operator darf Container steuern — aber nicht auf jedem Host.
func (i Identity) CanAccessHost(hostID string) bool {
	if i.Role == RoleAdmin || len(i.HostIDs) == 0 {
		return true
	}
	for _, id := range i.HostIDs {
		if id == hostID {
			return true
		}
	}
	return false
}

// Actor liefert die Bezeichnung für das Ereignisprotokoll.
func (i Identity) Actor() string {
	if i.ViaToken != "" {
		return i.Username + " (token: " + i.ViaToken + ")"
	}
	return i.Username
}

// Via beschreibt, woher die Rechte kamen. Gehört als Detail ins
// Ereignisprotokoll, nicht in den Actor selbst — sonst wird die Spalte
// unlesbar.
func (i Identity) Via() string {
	if len(i.Sources) == 0 {
		return "direct"
	}
	return strings.Join(i.Sources, "+")
}

// Argon2id-Parameter. Zeit- und Speicherbedarf sind bewusst so gewählt, dass
// sie auch auf einem Raspberry Pi in unter einer Sekunde bleiben — ein
// Anmeldevorgang darf einen kleinen Host nicht für Sekunden blockieren.
const (
	argonTime    = 2
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword erzeugt einen Argon2id-Hash im PHC-Format.
//
// Das Format enthält die Parameter, damit ein späterer Wechsel möglich ist,
// ohne bestehende Anmeldungen zu brechen.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt erzeugen: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// ErrBadPassword steht für „Anmeldung fehlgeschlagen" — bewusst ohne
// Unterscheidung zwischen unbekanntem Nutzer und falschem Passwort.
var ErrBadPassword = errors.New("benutzername oder passwort falsch")

// VerifyPassword prüft ein Passwort gegen einen gespeicherten Hash.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return fmt.Errorf("unbekanntes hash-format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return fmt.Errorf("hash-version unlesbar: %w", err)
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return fmt.Errorf("hash-parameter unlesbar: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("salt unlesbar: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("hash unlesbar: %w", err)
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want))) //nolint:gosec // G115: Hash-Länge, praktisch nie nahe 2^32

	// Konstantzeit-Vergleich: Ein Vergleich mit früherem Abbruch verrät über
	// die Laufzeit, wie viele Bytes übereinstimmen.
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrBadPassword
	}
	return nil
}

// MinPasswordLength ist die Untergrenze.
//
// Zwölf Zeichen ohne Zeichenklassen-Zwang: Vorgaben wie „mindestens ein
// Sonderzeichen" führen erfahrungsgemäß zu schlechteren Passwörtern, nicht zu
// besseren. Länge ist das, was zählt.
const MinPasswordLength = 12

func ValidatePassword(p string) error {
	if len([]rune(p)) < MinPasswordLength {
		return fmt.Errorf("passwort muss mindestens %d zeichen haben", MinPasswordLength)
	}
	if len(p) > 1024 {
		// Obergrenze gegen Rechenlast-Angriffe: Argon2 arbeitet über die
		// gesamte Eingabe.
		return errors.New("passwort ist zu lang")
	}
	return nil
}

// ValidateUsername prüft den Benutzernamen.
func ValidateUsername(u string) error {
	if len(u) < 2 || len(u) > 32 {
		return errors.New("benutzername muss 2 bis 32 zeichen haben")
	}
	for _, r := range u {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return errors.New("benutzername darf nur buchstaben, ziffern, punkt, minus und unterstrich enthalten")
		}
	}
	return nil
}

// NewSecret erzeugt ein zufälliges Geheimnis (Sitzungs- oder API-Token).
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("zufall erzeugen: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashSecret hasht Sitzungs- und API-Token vor der Ablage.
//
// SHA-256 ohne Salt ist hier korrekt: Es sind 256-Bit-Zufallswerte, keine
// Passwörter. Es gibt keinen rateraumarmen Eingaberaum, den ein Salt schützen
// müsste — anders als bei Passwörtern, wo Argon2id zum Einsatz kommt.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// GenerateInitialPassword erzeugt das Startpasswort des ersten Admins.
func GenerateInitialPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
