// Package memstore hält den Zustand im Arbeitsspeicher.
//
// Zweck: der Nachweis, dass die Austauschbarkeit echt ist. Es entstand nach
// dem SQLite-Backend, besteht dieselbe Konformitätssuite und brauchte dafür
// keine Zeile Änderung an fremdem Code (ADR-0031).
//
// **Nicht für den Betrieb.** Ein Neustart verliert alles: Hosts müssten sich
// neu enrollen, Nutzer wären weg. Brauchbar für schnelle Tests und um eine
// Oberfläche ohne Datenbank auszuprobieren.
//
// Wer ein echtes zweites Backend baut — Postgres etwa —, findet hier die
// vollständige Liste der zu erfüllenden Methoden und in storetest die Prüfung.
package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aronk11/havenry/internal/store"
)

func init() {
	store.Register("memory", func(context.Context, string) (store.Full, error) {
		return New(), nil
	})
}

type Mem struct {
	mu      sync.Mutex
	tokens  map[string]store.EnrollToken
	hosts   map[string]store.Host
	users   map[string]store.User
	sess    map[string]store.Session
	apitok  map[string]store.APIToken
	teams   map[string]store.Team
	members map[string]map[string]time.Time
	events  []store.Event
	eventID int64
	repo    *store.GitRepo
}

func New() *Mem {
	return &Mem{
		tokens: map[string]store.EnrollToken{}, hosts: map[string]store.Host{},
		users: map[string]store.User{}, sess: map[string]store.Session{},
		apitok: map[string]store.APIToken{}, teams: map[string]store.Team{},
		members: map[string]map[string]time.Time{},
	}
}

func (m *Mem) CreateEnrollToken(_ context.Context, t store.EnrollToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[t.TokenHash] = t
	return nil
}

func (m *Mem) ConsumeEnrollToken(_ context.Context, hash string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[hash]
	if !ok {
		return store.ErrNotFound
	}
	if t.UsedAt != nil {
		return store.ErrTokenUsed
	}
	if now.After(t.ExpiresAt) {
		return store.ErrTokenExpired
	}
	t.UsedAt = &now
	m.tokens[hash] = t
	return nil
}

func (m *Mem) UpsertHost(_ context.Context, h store.Host) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.hosts[h.ID]; ok {
		h.Approved, h.EnrolledAt = old.Approved, old.EnrolledAt
	}
	m.hosts[h.ID] = h
	return nil
}

func (m *Mem) HostByID(_ context.Context, id string) (store.Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hosts[id]
	if !ok {
		return store.Host{}, store.ErrNotFound
	}
	return h, nil
}

func (m *Mem) HostByCredentialHash(_ context.Context, hash string) (store.Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range m.hosts {
		if h.CredentialHash == hash {
			return h, nil
		}
	}
	return store.Host{}, store.ErrNotFound
}

func (m *Mem) Hosts(context.Context) ([]store.Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.Host{}
	for _, h := range m.hosts {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

func (m *Mem) ApproveHost(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hosts[id]
	if !ok {
		return store.ErrNotFound
	}
	h.Approved = true
	m.hosts[id] = h
	return nil
}

func (m *Mem) TouchHost(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hosts[id]
	if !ok {
		return store.ErrNotFound
	}
	h.LastSeen = at
	m.hosts[id] = h
	return nil
}

func (m *Mem) AppendEvent(_ context.Context, e store.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventID++
	e.ID = m.eventID
	m.events = append(m.events, e)
	return nil
}

func (m *Mem) Events(_ context.Context, limit int) ([]store.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	out := make([]store.Event, limit)
	copy(out, m.events[len(m.events)-limit:])
	return out, nil
}

func (m *Mem) CreateUser(_ context.Context, u store.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.users {
		if strings.EqualFold(e.Username, u.Username) {
			return store.ErrUserExists
		}
	}
	m.users[u.ID] = u
	return nil
}

func (m *Mem) UpdateUser(_ context.Context, u store.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; !ok {
		return store.ErrNotFound
	}
	for id, e := range m.users {
		if id != u.ID && strings.EqualFold(e.Username, u.Username) {
			return store.ErrUserExists
		}
	}
	m.users[u.ID] = u
	return nil
}

func (m *Mem) DeleteUser(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.users, id)
	for h, s := range m.sess {
		if s.UserID == id {
			delete(m.sess, h)
		}
	}
	for h, t := range m.apitok {
		if t.UserID == id {
			delete(m.apitok, h)
		}
	}
	for _, mem := range m.members {
		delete(mem, id)
	}
	return nil
}

func (m *Mem) UserByID(_ context.Context, id string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (m *Mem) UserByName(_ context.Context, name string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Username, name) {
			return u, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

func (m *Mem) Users(context.Context) ([]store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.User{}
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (m *Mem) CountUsers(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *Mem) TouchLogin(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return store.ErrNotFound
	}
	u.LastLogin = &at
	m.users[id] = u
	return nil
}

func (m *Mem) CreateSession(_ context.Context, s store.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sess[s.TokenHash] = s
	return nil
}

func (m *Mem) SessionByHash(_ context.Context, hash string) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sess[hash]
	if !ok || time.Now().After(s.ExpiresAt) {
		return store.Session{}, store.ErrNotFound
	}
	return s, nil
}

func (m *Mem) DeleteSession(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sess, hash)
	return nil
}

func (m *Mem) DeleteUserSessions(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, s := range m.sess {
		if s.UserID == userID {
			delete(m.sess, h)
		}
	}
	return nil
}

func (m *Mem) PurgeExpiredSessions(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, s := range m.sess {
		if now.After(s.ExpiresAt) {
			delete(m.sess, h)
		}
	}
	return nil
}

func (m *Mem) CreateAPIToken(_ context.Context, t store.APIToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apitok[t.TokenHash] = t
	return nil
}

func (m *Mem) APITokenByHash(_ context.Context, hash string) (store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.apitok[hash]
	if !ok {
		return store.APIToken{}, store.ErrNotFound
	}
	if t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt) {
		return store.APIToken{}, store.ErrTokenExpired
	}
	return t, nil
}

func (m *Mem) APITokensByUser(_ context.Context, userID string) ([]store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.APIToken{}
	for _, t := range m.apitok {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *Mem) DeleteAPIToken(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, t := range m.apitok {
		if t.ID == id {
			delete(m.apitok, h)
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *Mem) TouchAPIToken(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, t := range m.apitok {
		if t.ID == id {
			t.LastUsed = &at
			m.apitok[h] = t
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *Mem) SaveRepo(_ context.Context, r store.GitRepo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repo = &r
	return nil
}

func (m *Mem) Repo(context.Context) (store.GitRepo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.repo == nil {
		return store.GitRepo{}, store.ErrNotFound
	}
	return *m.repo, nil
}

func (m *Mem) ClearRepo(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repo = nil
	return nil
}

func (m *Mem) CreateTeam(_ context.Context, t store.Team) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.teams {
		if strings.EqualFold(e.Name, t.Name) {
			return store.ErrTeamExists
		}
	}
	m.teams[t.ID] = t
	return nil
}

func (m *Mem) UpdateTeam(_ context.Context, t store.Team) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.teams[t.ID]; !ok {
		return store.ErrNotFound
	}
	m.teams[t.ID] = t
	return nil
}

func (m *Mem) DeleteTeam(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.teams[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.teams, id)
	delete(m.members, id)
	return nil
}

func (m *Mem) TeamByID(_ context.Context, id string) (store.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.teams[id]
	if !ok {
		return store.Team{}, store.ErrNotFound
	}
	return t, nil
}

func (m *Mem) Teams(context.Context) ([]store.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.Team{}
	for _, t := range m.teams {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Mem) AddTeamMember(_ context.Context, teamID, userID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.teams[teamID]; !ok {
		return store.ErrNotFound
	}
	if _, ok := m.users[userID]; !ok {
		return store.ErrNotFound
	}
	if m.members[teamID] == nil {
		m.members[teamID] = map[string]time.Time{}
	}
	m.members[teamID][userID] = at
	return nil
}

func (m *Mem) RemoveTeamMember(_ context.Context, teamID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem, ok := m.members[teamID]
	if !ok {
		return store.ErrNotFound
	}
	if _, ok := mem[userID]; !ok {
		return store.ErrNotFound
	}
	delete(mem, userID)
	return nil
}

func (m *Mem) TeamMembers(_ context.Context, teamID string) ([]store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.User{}
	for uid := range m.members[teamID] {
		if u, ok := m.users[uid]; ok {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (m *Mem) TeamsForUser(_ context.Context, userID string) ([]store.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.Team{}
	for tid, mem := range m.members {
		if _, ok := mem[userID]; ok {
			if t, ok := m.teams[tid]; ok {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var _ store.Full = (*Mem)(nil)
