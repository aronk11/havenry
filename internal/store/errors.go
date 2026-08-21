package store

import "errors"

var (
	// ErrTokenUsed bedeutet: das Enrollment-Token wurde bereits eingelöst.
	ErrTokenUsed = errors.New("enrollment-token bereits verwendet")
	// ErrTokenExpired bedeutet: das Token ist abgelaufen (Default 15 Minuten).
	ErrTokenExpired = errors.New("enrollment-token abgelaufen")
)

// ErrUserExists bedeutet: der Benutzername ist bereits vergeben.
var ErrUserExists = errors.New("benutzername bereits vergeben")

// ErrTeamExists bedeutet: der Teamname ist bereits vergeben.
var ErrTeamExists = errors.New("teamname bereits vergeben")
