package api

import (
	"fmt"
	"sync"
	"time"
)

// CrewMember is a deliberately small resource: enough fields to make
// GET/POST/DELETE against /api/crew a believable REST CRUD target for
// load-testing tutorials, without modeling anything real.
type CrewMember struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Role    string    `json:"role"`
	AddedAt time.Time `json:"added_at"`
}

// LaunchStatus mirrors the coarse lifecycle a launch moves through.
type LaunchStatus string

const (
	StatusScheduled LaunchStatus = "scheduled"
	StatusInFlight  LaunchStatus = "in_flight"
	StatusComplete  LaunchStatus = "complete"
	StatusAborted   LaunchStatus = "aborted"
)

// Launch is the primary resource mission-control exposes over REST. Its
// Ignite/Abort transitions are what drive gRPC calls to the engine service
// and telemetry frames out over WebSocket.
type Launch struct {
	ID             string       `json:"id"`
	Vehicle        string       `json:"vehicle"`
	CrewIDs        []string     `json:"crew_ids"`
	TargetThrustKN float64      `json:"target_thrust_kn"`
	DurationSec    int32        `json:"duration_seconds"`
	Status         LaunchStatus `json:"status"`
	ScheduledAt    time.Time    `json:"scheduled_at"`
}

// Store is a plain in-memory store, protected by a mutex. LaunchPad is a
// load-testing fixture, not a durability demo, so there is deliberately no
// database here — restarting the process resets all state.
type Store struct {
	mu     sync.RWMutex
	crew   map[string]*CrewMember
	launch map[string]*Launch
	nextID int
}

func NewStore() *Store {
	return &Store{
		crew:   make(map[string]*CrewMember),
		launch: make(map[string]*Launch),
	}
}

func (s *Store) newID(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s-%04d", prefix, s.nextID)
}

func (s *Store) AddCrew(name, role string) *CrewMember {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := &CrewMember{ID: s.newID("crew"), Name: name, Role: role, AddedAt: time.Now().UTC()}
	s.crew[m.ID] = m
	return m
}

func (s *Store) ListCrew() []*CrewMember {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CrewMember, 0, len(s.crew))
	for _, m := range s.crew {
		out = append(out, m)
	}
	return out
}

func (s *Store) GetCrew(id string) (*CrewMember, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.crew[id]
	return m, ok
}

func (s *Store) DeleteCrew(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.crew[id]; !ok {
		return false
	}
	delete(s.crew, id)
	return true
}

func (s *Store) ScheduleLaunch(vehicle string, crewIDs []string, thrust float64, duration int32) *Launch {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := &Launch{
		ID:             s.newID("launch"),
		Vehicle:        vehicle,
		CrewIDs:        crewIDs,
		TargetThrustKN: thrust,
		DurationSec:    duration,
		Status:         StatusScheduled,
		ScheduledAt:    time.Now().UTC(),
	}
	s.launch[l.ID] = l
	return l
}

func (s *Store) ListLaunches() []*Launch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Launch, 0, len(s.launch))
	for _, l := range s.launch {
		out = append(out, l)
	}
	return out
}

func (s *Store) GetLaunch(id string) (*Launch, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.launch[id]
	return l, ok
}

func (s *Store) SetLaunchStatus(id string, status LaunchStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.launch[id]; ok {
		l.Status = status
	}
}
