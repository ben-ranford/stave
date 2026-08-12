package secret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

var (
	ErrEmptySecret   = errors.New("empty secret")
	ErrInvalidHandle = errors.New("invalid secret handle")
	ErrUnavailable   = errors.New("secret unavailable")
)

type Handle struct {
	ID string `json:"id"`
}
type Store interface {
	Put([]byte) (Handle, error)
	Use(Handle, func([]byte) error) error
	Destroy(Handle) error
}

// MemoryStore is an opaque, single-process secret provider. Values are never JSON-marshaled.
type MemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
	used   map[string]bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: map[string][]byte{}, used: map[string]bool{}}
}
func (s *MemoryStore) Put(value []byte) (Handle, error) {
	if len(value) == 0 {
		return Handle{}, ErrEmptySecret
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return Handle{}, e
	}
	id := hex.EncodeToString(b)
	cp := append([]byte(nil), value...)
	s.mu.Lock()
	s.values[id] = cp
	s.mu.Unlock()
	return Handle{ID: id}, nil
}
func (s *MemoryStore) Use(h Handle, fn func([]byte) error) error {
	if h.ID == "" || fn == nil {
		return ErrInvalidHandle
	}
	s.mu.Lock()
	v, ok := s.values[h.ID]
	if !ok || s.used[h.ID] {
		s.mu.Unlock()
		return ErrUnavailable
	}
	s.used[h.ID] = true
	delete(s.values, h.ID)
	cp := append([]byte(nil), v...)
	for i := range v {
		v[i] = 0
	}
	s.mu.Unlock()
	err := fn(cp)
	for i := range cp {
		cp[i] = 0
	}
	return err
}
func (s *MemoryStore) Destroy(h Handle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[h.ID]
	if !ok {
		if s.used[h.ID] {
			return nil
		}
		return ErrUnavailable
	}
	for i := range v {
		v[i] = 0
	}
	delete(s.values, h.ID)
	s.used[h.ID] = true
	return nil
}
