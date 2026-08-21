package controlplane

import (
	"context"
	"net/http"

	"github.com/aronk11/havenry/internal/auth"
)

// Der Schutz des letzten Admins muss seit ADR-0029 Teams mitzählen.
//
// Vorher genügte ein Blick auf die Rolle am Nutzer. Jetzt kann ein Adminrecht
// ausschließlich aus einer Mitgliedschaft stammen — wer nur die Direktrolle
// prüft, hält eine Installation für sicher, die sich mit dem nächsten Klick
// aussperrt.
//
// Der Ansatz ist überall derselbe: Den Zustand nach der geplanten Änderung
// durchrechnen und fragen, ob dann noch jemand Admin wäre. Das ist ehrlicher
// als eine Sammlung von Sonderfällen.

// adminUsersAfter zählt die Nutzer, die nach einer gedachten Änderung noch
// wirksame Adminrechte hätten.
//
// exclude bekommt Nutzer und Rechtequelle und meldet, ob diese Quelle
// wegfällt. So lässt sich dieselbe Rechnung für "Team löschen",
// "Team herabstufen", "Mitglied entfernen" und "Nutzer herabstufen"
// verwenden.
func (s *Server) adminUsersAfter(
	ctx context.Context,
	exclude func(userID string, g auth.Grant) bool,
) (int, error) {
	users, err := s.store.Users(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, u := range users {
		grants, err := s.auth.grantsFor(ctx, u)
		if err != nil {
			return 0, err
		}
		kept := make([]auth.Grant, 0, len(grants))
		for _, g := range grants {
			if exclude != nil && exclude(u.ID, g) {
				continue
			}
			kept = append(kept, g)
		}
		if auth.Resolve(u.ID, u.Username, kept).Role == auth.RoleAdmin {
			count++
		}
	}
	return count, nil
}

// wouldStrandInstallation meldet, ob nach der Änderung kein Admin mehr übrig
// wäre. Der Name sagt, worum es geht: nicht um Bürokratie, sondern darum, dass
// die Installation danach nicht mehr verwaltbar ist.
func (s *Server) wouldStrandInstallation(
	r *http.Request,
	exclude func(userID string, g auth.Grant) bool,
) bool {
	n, err := s.adminUsersAfter(r.Context(), exclude)
	if err != nil {
		// Im Zweifel blockieren. Eine abgelehnte Änderung ist ärgerlich, eine
		// unverwaltbare Installation ist ein Totalausfall.
		s.logger.Error("adminzählung fehlgeschlagen — änderung vorsorglich abgelehnt", "fehler", err)
		return true
	}
	return n == 0
}

// excludeTeam entfernt alle Rechte, die aus einem bestimmten Team stammen.
func excludeTeam(teamName string) func(string, auth.Grant) bool {
	return func(_ string, g auth.Grant) bool {
		return g.Source == "team:"+teamName
	}
}

// excludeTeamForUser entfernt die Rechte eines Teams für genau einen Nutzer —
// der Fall "Mitglied entfernen".
func excludeTeamForUser(teamName, userID string) func(string, auth.Grant) bool {
	return func(uid string, g auth.Grant) bool {
		return uid == userID && g.Source == "team:"+teamName
	}
}

// excludeDirectForUser entfernt die Direktzuweisung eines Nutzers — der Fall
// "herabstufen" oder "löschen".
func excludeDirectForUser(userID string) func(string, auth.Grant) bool {
	return func(uid string, g auth.Grant) bool {
		return uid == userID && g.Source == "direct"
	}
}

// excludeUser entfernt einen Nutzer vollständig — der Fall "löschen".
func excludeUser(userID string) func(string, auth.Grant) bool {
	return func(uid string, _ auth.Grant) bool {
		return uid == userID
	}
}
