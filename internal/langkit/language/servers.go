package language

import (
	"context"
	"sync"
)

// Servers routes a file to the client that handles it.
//
// One client per language server, keyed by the extensions its spec claims. The
// router is what makes degradation automatic: For never returns nil and never
// returns an error, so the editor asks "who handles this file" and gets an
// answer whether or not anything is installed.
type Servers struct {
	mu sync.RWMutex // protects byExtension and all
	// byExtension maps a lower-cased extension, leading dot included, to the
	// client that claims it. The FIRST claim wins: two servers fighting over
	// .go is a configuration mistake, and picking one predictably beats
	// picking whichever map iteration reached first.
	byExtension map[string]Client
	all         []Client
}

// NewServers builds an empty router.
//
// Returns the concrete type deliberately, against Rule 7.2: this is a
// container with no alternative implementation and no behaviour to mock — the
// thing worth mocking is the Client it hands back, which IS an interface.
func NewServers() *Servers {
	return &Servers{byExtension: map[string]Client{}}
}

// Register claims a spec's extensions for a client.
//
// An extension already claimed is left alone, and a spec claiming none
// registers the client for lifecycle purposes without routing anything to it.
func (s *Servers) Register(spec ServerSpec, client Client) {
	if client == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.all = append(s.all, client)
	for _, ext := range spec.Extensions {
		key := NormaliseExtension(ext)
		if key == "" || s.byExtension[key] != nil {
			continue
		}
		s.byExtension[key] = client
	}
}

// For is the client that handles a path, or Absent.
//
// NEVER nil and never an error: an editor asking who handles a README must get
// something it can call, or every call site grows a branch.
func (s *Servers) For(path string) Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if client, ok := s.byExtension[Extension(path)]; ok {
		return client
	}
	return Absent()
}

// StopAll shuts every registered server down, and is what a closing window
// calls. One server refusing to leave must not stop the others being asked.
func (s *Servers) StopAll(ctx context.Context) {
	for _, client := range s.clients() {
		_ = client.Stop(ctx)
	}
}

// clients copies the registered set so a Stop that takes time is not holding
// the router's lock while it does.
func (s *Servers) clients() []Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Client{}, s.all...)
}

// Reset forgets every client and every route, returning the clients it held so
// the caller can stop them.
//
// For a project switch: a language server is initialised against ONE workspace
// root, and a server still indexing the project the window left answers every
// question about the one it opened with the wrong module. The caller stops the
// old clients and registers servers rooted at the new project.
func (s *Servers) Reset() []Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.all
	s.all, s.byExtension = nil, map[string]Client{}
	return old
}
