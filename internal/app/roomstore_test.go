package app

import (
	"sync"
	"testing"

	"github.com/arturskrzydlo/chat-room/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to make a dummy room
func newTestRoom(id string) *models.Room {
	return models.NewRoom(id, "name_"+id, "author_"+id)
}

func TestRoomStoreBasicOperations(t *testing.T) {
	s := newRoomStore()

	// Store and Load
	r1 := newTestRoom("room1")
	s.Store("room1", r1)

	got, ok := s.Load("room1")
	require.True(t, ok)
	assert.Same(t, r1, got)

	// Load non-existent
	got, ok = s.Load("nope")
	require.False(t, ok)
	assert.Nil(t, got)

	// Delete
	s.Delete("room1")
	got, ok = s.Load("room1")
	require.False(t, ok)
	assert.Nil(t, got)
}

func TestRoomStoreRange(t *testing.T) {
	s := newRoomStore()

	r1 := newTestRoom("room1")
	r2 := newTestRoom("room2")
	s.Store("room1", r1)
	s.Store("room2", r2)

	seen := make(map[string]bool)
	s.Range(func(r *models.Room) bool {
		seen[r.ID] = true
		return true
	})

	assert.True(t, seen["room1"])
	assert.True(t, seen["room2"])
	assert.Len(t, seen, 2)
}

func TestRoomStoreConcurrentAccess(t *testing.T) {
	s := newRoomStore()

	const goroutines = 20
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // writers + readers

	// Writers: concurrent Store/Delete on different keys
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				id := "room_w_" + string(rune('A'+g)) + "_" + string(rune('a'+i%10))
				s.Store(id, newTestRoom(id))
				s.Delete(id)
			}
		}(g)
	}

	// Readers: concurrent Load/Range
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				s.Load("some_room") // may or may not exist
				s.Range(func(r *models.Room) bool {
					_ = r.ID
					return true
				})
			}
		}()
	}

	wg.Wait()

	// No assertion needed beyond "no data race / panic".
	// But we can at least call Range and ensure it doesn't explode.
	s.Range(func(r *models.Room) bool { return true })
}
