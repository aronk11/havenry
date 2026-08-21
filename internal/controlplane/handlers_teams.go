package controlplane

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aronk11/havenry/internal/auth"
	"github.com/aronk11/havenry/internal/store"
)

type teamView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Role        string    `json:"role"`
	HostIDs     []string  `json:"host_ids"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

func toTeamView(t store.Team, members int) teamView {
	ids := t.HostIDs
	if ids == nil {
		ids = []string{}
	}
	return teamView{
		ID: t.ID, Name: t.Name, Description: t.Description, Role: t.Role,
		HostIDs: ids, MemberCount: members, CreatedAt: t.CreatedAt,
	}
}

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.store.Teams(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]teamView, 0, len(teams))
	for _, t := range teams {
		members, _ := s.store.TeamMembers(r.Context(), t.ID)
		out = append(out, toTeamView(t, len(members)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": out})
}

func (s *Server) getTeam(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.TeamByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	members, err := s.store.TeamMembers(r.Context(), t.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	mv := make([]userView, 0, len(members))
	for _, m := range members {
		mv = append(mv, toUserView(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"team": toTeamView(t, len(members)), "members": mv,
	})
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Role        string   `json:"role"`
		HostIDs     []string `json:"host_ids"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := auth.ValidateUsername(body.Name); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("teamname: %w", err))
		return
	}
	role := auth.Role(body.Role)
	if !role.Valid() {
		writeErr(w, http.StatusBadRequest,
			fmt.Errorf("rolle %q unbekannt (erlaubt: admin, operator, viewer)", body.Role))
		return
	}

	id, err := newID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	t := store.Team{
		ID: id, Name: body.Name, Description: body.Description,
		Role: string(role), HostIDs: body.HostIDs, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateTeam(r.Context(), t); err != nil {
		if errors.Is(err, store.ErrTeamExists) {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	s.logAction(r, "teams.created", "",
		fmt.Sprintf("Team %q angelegt (%s)", t.Name, t.Role))
	writeJSON(w, http.StatusCreated, map[string]any{"team": toTeamView(t, 0)})
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        *string   `json:"name"`
		Description *string   `json:"description"`
		Role        *string   `json:"role"`
		HostIDs     *[]string `json:"host_ids"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	t, err := s.store.TeamByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	// Ein Team, das Adminrechte trägt, darf nicht herabgestuft werden, wenn
	// danach niemand mehr Admin wäre.
	if body.Role != nil && *body.Role != t.Role && t.Role == string(auth.RoleAdmin) {
		if s.wouldStrandInstallation(r, excludeTeam(t.Name)) {
			writeErr(w, http.StatusConflict, errors.New(
				"danach hätte niemand mehr Adminrechte — vorher einen weiteren Admin einrichten"))
			return
		}
	}

	if body.Name != nil {
		if err := auth.ValidateUsername(*body.Name); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("teamname: %w", err))
			return
		}
		t.Name = *body.Name
	}
	if body.Description != nil {
		t.Description = *body.Description
	}
	if body.Role != nil {
		role := auth.Role(*body.Role)
		if !role.Valid() {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("rolle %q unbekannt", *body.Role))
			return
		}
		t.Role = string(role)
	}
	if body.HostIDs != nil {
		t.HostIDs = *body.HostIDs
	}

	if err := s.store.UpdateTeam(r.Context(), t); err != nil {
		if errors.Is(err, store.ErrTeamExists) {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.endSessionsOfTeam(r, t.ID)

	s.logAction(r, "teams.updated", "", fmt.Sprintf("Team %q geändert", t.Name))
	writeJSON(w, http.StatusOK, map[string]any{"team": toTeamView(t, 0)})
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.TeamByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if t.Role == string(auth.RoleAdmin) && s.wouldStrandInstallation(r, excludeTeam(t.Name)) {
		writeErr(w, http.StatusConflict, errors.New(
			"danach hätte niemand mehr Adminrechte — vorher einen weiteren Admin einrichten"))
		return
	}

	// Sitzungen VOR dem Löschen beenden — danach sind die Mitgliedschaften weg
	// und niemand mehr auffindbar.
	s.endSessionsOfTeam(r, t.ID)

	if err := s.store.DeleteTeam(r.Context(), t.ID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.logAction(r, "teams.deleted", "", fmt.Sprintf("Team %q gelöscht", t.Name))
	writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

func (s *Server) addTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID, userID := r.PathValue("id"), r.PathValue("userID")

	if err := s.store.AddTeamMember(r.Context(), teamID, userID, time.Now().UTC()); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	// Neue Rechte sollen sofort gelten, nicht erst nach dem nächsten Anmelden.
	_ = s.store.DeleteUserSessions(r.Context(), userID)

	t, _ := s.store.TeamByID(r.Context(), teamID)
	u, _ := s.store.UserByID(r.Context(), userID)
	s.logAction(r, "teams.member_added", "",
		fmt.Sprintf("%s zu Team %q hinzugefügt", u.Username, t.Name))
	writeJSON(w, http.StatusOK, map[string]string{"status": "hinzugefügt"})
}

func (s *Server) removeTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID, userID := r.PathValue("id"), r.PathValue("userID")

	t, err := s.store.TeamByID(r.Context(), teamID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if t.Role == string(auth.RoleAdmin) &&
		s.wouldStrandInstallation(r, excludeTeamForUser(t.Name, userID)) {
		writeErr(w, http.StatusConflict, errors.New(
			"danach hätte niemand mehr Adminrechte"))
		return
	}

	if err := s.store.RemoveTeamMember(r.Context(), teamID, userID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	_ = s.store.DeleteUserSessions(r.Context(), userID)

	u, _ := s.store.UserByID(r.Context(), userID)
	s.logAction(r, "teams.member_removed", "",
		fmt.Sprintf("%s aus Team %q entfernt", u.Username, t.Name))
	writeJSON(w, http.StatusOK, map[string]string{"status": "entfernt"})
}

// endSessionsOfTeam meldet alle Mitglieder ab.
//
// Eine Änderung am Team ändert die Rechte seiner Mitglieder. Eine offene
// Sitzung würde sonst die alten weitertragen — dasselbe Prinzip wie beim
// Rollenwechsel in ADR-0022.
func (s *Server) endSessionsOfTeam(r *http.Request, teamID string) {
	members, err := s.store.TeamMembers(r.Context(), teamID)
	if err != nil {
		return
	}
	for _, m := range members {
		_ = s.store.DeleteUserSessions(r.Context(), m.ID)
	}
}

// logAction schreibt einen Protokolleintrag mit Auslöser und Rechtequelle.
func (s *Server) logAction(r *http.Request, kind, hostID, summary string) {
	id := identityFrom(r.Context())
	_ = s.store.AppendEvent(r.Context(), store.Event{
		At: time.Now().UTC(), HostID: hostID, Kind: kind,
		Actor: id.Actor(), Summary: summary,
		Details: map[string]string{"via": id.Via()},
	})
}
