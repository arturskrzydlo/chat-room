package coordinator

import (
	"sync"
)

type roomStore struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func newRoomStore() *roomStore {
	return &roomStore{
		rooms: make(map[string]*Room),
	}
}

func (s *roomStore) Load(id string) (*Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rooms[id]
	return r, ok
}

func (s *roomStore) Store(id string, r *Room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rooms[id] = r
}

func (s *roomStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rooms, id)
}

func (s *roomStore) Range(f func(*Room) bool) {
	s.mu.RLock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.mu.RUnlock()

	for _, r := range rooms {
		if !f(r) {
			return
		}
	}
}
