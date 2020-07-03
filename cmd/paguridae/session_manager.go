package main

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

type SessionManager struct {
	sessions      map[uuid.UUID]*Session
	mux           sync.Mutex
	verifyContent bool
}

func NewSessionManager(verifyContent bool, sessionPurgeSeconds int) *SessionManager {
	m := &SessionManager{
		sessions:      make(map[uuid.UUID]*Session),
		verifyContent: verifyContent,
	}
	go func() {
		emptySessions := make(map[uuid.UUID]time.Time)
		for {
			time.Sleep(10 * time.Minute)
			now := time.Now()
			sessionsToPurge := make([]*Session, 0)
			m.mux.Lock()
			for sessionId, session := range m.sessions {
				if session.Connections() == 0 {
					emptyAt, ok := emptySessions[sessionId]
					if ok {
						if now.After(emptyAt.Add(time.Duration(sessionPurgeSeconds) * time.Second)) {
							sessionsToPurge = append(sessionsToPurge, session)
						}
					} else {
						emptySessions[sessionId] = now
					}
				} else {
					delete(emptySessions, sessionId)
				}
			}
			for _, session := range sessionsToPurge {
				log.Printf("Terminating session: %s", session.Id())
				delete(m.sessions, session.Id())
				delete(emptySessions, session.Id())
				session.Stop()
			}
			m.mux.Unlock()
		}
	}()
	return m
}

func (m *SessionManager) FindOrCreateSession(id *uuid.UUID) (*Session, error) {
	m.mux.Lock()
	defer m.mux.Unlock()

	var session *Session
	if id != nil {
		session = m.sessions[*id]
	}
	if session == nil {
		var err error
		session, err = NewSession(m.verifyContent)
		if err != nil {
			return nil, err
		}
		m.sessions[session.Id()] = session
	}
	return session, nil
}
