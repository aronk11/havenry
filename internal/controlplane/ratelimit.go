package controlplane

import (
	"sync"
	"time"
)

// Anmeldeversuche werden gebremst.
//
// Ohne Bremse ist ein Passwort mit zwölf Zeichen zwar nicht in Minuten zu
// raten, aber ein Angreifer mit einer Liste geleakter Passwörter braucht keine
// Vollsuche — er braucht nur Versuche. Protokollieren allein hilft nicht, wenn
// niemand hinschaut.
const (
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute
	loginLockout     = 15 * time.Minute
)

// loginLimiter bremst Anmeldeversuche je Schlüssel.
//
// Gezählt wird pro Benutzername UND pro Quelladresse. Nur nach Adresse zu
// zählen hilft nicht gegen verteilte Versuche; nur nach Benutzername zu zählen
// erlaubt es, fremde Konten gezielt auszusperren. Beides zusammen deckt die
// realistischen Fälle ab, ohne dass ein Angreifer den legitimen Nutzer
// dauerhaft blockieren kann — dessen eigene Adresse bleibt frei.
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
}

type limiterEntry struct {
	attempts    int
	firstFailed time.Time
	lockedUntil time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{entries: make(map[string]*limiterEntry), now: time.Now}
}

// Allowed meldet, ob ein Versuch zulässig ist, und wie lange sonst zu warten ist.
func (l *loginLimiter) Allowed(keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	for _, k := range keys {
		e, ok := l.entries[k]
		if !ok {
			continue
		}
		if now.Before(e.lockedUntil) {
			return false, e.lockedUntil.Sub(now).Round(time.Second)
		}
	}
	return true, 0
}

// Fail vermerkt einen Fehlversuch.
func (l *loginLimiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	for _, k := range keys {
		e, ok := l.entries[k]
		if !ok || now.Sub(e.firstFailed) > loginWindow {
			l.entries[k] = &limiterEntry{attempts: 1, firstFailed: now}
			continue
		}
		e.attempts++
		if e.attempts >= loginMaxAttempts {
			e.lockedUntil = now.Add(loginLockout)
			e.attempts = 0
			e.firstFailed = now
		}
	}
	l.gc(now)
}

// Succeed setzt die Zähler nach erfolgreicher Anmeldung zurück.
func (l *loginLimiter) Succeed(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.entries, k)
	}
}

// gc hält die Tabelle klein. Ohne Aufräumen wächst sie mit jedem je
// versuchten Benutzernamen — ein einfacher Weg, den Speicher zu füllen.
func (l *loginLimiter) gc(now time.Time) {
	if len(l.entries) < 1000 {
		return
	}
	for k, e := range l.entries {
		if now.After(e.lockedUntil) && now.Sub(e.firstFailed) > loginWindow {
			delete(l.entries, k)
		}
	}
}
