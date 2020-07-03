package ot

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/google/uuid"
)

type UpdateFunction func(d delta.Delta) (delta.Delta, error)
type UpdateAllFunction func(contents []ServerUpdate) ([]ClientChange, error)

type ServerUpdate struct {
	Id                         uint32      `json:"id"`
	Delta                      delta.Delta `json:"delta"`
	Base                       uint32      `json:"base"`
	Version                    uint32      `json:"version"`
	LastCommittedClientVersion *uint32     `json:"last_committed_client_version"`
}

type ClientChange struct {
	Id            uint32      `json:"id"`
	Delta         delta.Delta `json:"delta"`
	Base          uint32      `json:"base"`
	ClientVersion uint32      `json:"client_version"`
}

type Event struct {
	ConnectedClientId *uuid.UUID
	Updates           []ServerUpdate
	CreatedFileIds    []uint32
	ClosedFileIds     []uint32
}

type Server struct {
	nextFileId uint32
	files      map[uint32]*File
	clients    map[uuid.UUID]*client

	commands     chan command
	stoppingChan chan bool

	running int32

	ErrorProcessor func(error)
}

func NewServer() *Server {
	return &Server{
		nextFileId:     0,
		files:          make(map[uint32]*File),
		clients:        make(map[uuid.UUID]*client),
		commands:       make(chan command),
		stoppingChan:   make(chan bool),
		running:        0,
		ErrorProcessor: nil,
	}
}

func (s *Server) Connect(clientId *uuid.UUID) <-chan Event {
	events := make(chan Event)

	s.commands <- command{
		t:        typeConnect,
		clientId: clientId,
		events:   events,
	}

	return events
}

func (s *Server) Disconnect(clientId uuid.UUID) {
	s.commands <- command{
		t:        typeDisconnect,
		clientId: &clientId,
	}
}

func (s *Server) CreateFiles(contents ...delta.Delta) []uint32 {
	c := make(chan []uint32)

	s.commands <- command{
		t:          typeCreateFiles,
		contents:   contents,
		fileIdChan: c,
	}

	return <-c
}

func (s *Server) CloseFiles(fileIds ...uint32) {
	s.commands <- command{
		t:       typeCloseFiles,
		fileIds: fileIds,
	}
}

func (s *Server) Acks(clientId uuid.UUID, acks map[uint32]uint32) {
	s.commands <- command{
		t:        typeAck,
		clientId: &clientId,
		acks:     acks,
	}
}

func (s *Server) Content(fileId uint32) *ServerUpdate {
	c := make(chan []ServerUpdate)

	s.commands <- command{
		t:       typeContent,
		fileId:  fileId,
		updates: c,
	}

	updates := <-c
	if len(updates) > 0 {
		return &updates[0]
	} else {
		return nil
	}
}

func (s *Server) AllContents() []ServerUpdate {
	c := make(chan []ServerUpdate)

	s.commands <- command{
		t:       typeAllContents,
		updates: c,
	}

	return <-c
}

func (s *Server) Submit(clientId *uuid.UUID, changes ...ClientChange) {
	s.commands <- command{
		t:        typeSubmit,
		clientId: clientId,
		changes:  changes,
	}
}

func (s *Server) Undo(fileId uint32) error {
	c := make(chan error)

	s.commands <- command{
		t:         typeUndo,
		fileId:    fileId,
		errorChan: c,
	}

	return <-c
}

func (s *Server) Redo(fileId uint32) error {
	c := make(chan error)

	s.commands <- command{
		t:         typeRedo,
		fileId:    fileId,
		errorChan: c,
	}

	return <-c
}

func (s *Server) Update(fileId uint32, f UpdateFunction) error {
	c := make(chan error)

	s.commands <- command{
		t:          typeUpdate,
		fileId:     fileId,
		updateFunc: f,
		errorChan:  c,
	}

	return <-c
}

func (s *Server) UpdateAll(f UpdateAllFunction) error {
	c := make(chan error)

	s.commands <- command{
		t:             typeUpdateAll,
		updateAllFunc: f,
		errorChan:     c,
	}

	return <-c
}

func (s *Server) Append(fileId uint32, text []rune) {
	s.Update(fileId, func(d delta.Delta) (delta.Delta, error) {
		return *delta.New(nil).Retain(d.Length(), nil).Insert(string(text), nil), nil
	})
}

func (s *Server) Broadcast() {
	s.commands <- command{
		t: typeBroadcast,
	}
}

func (s *Server) Running() bool {
	return atomic.LoadInt32(&s.running) != 0
}

func (s *Server) Stop() {
	s.stoppingChan <- true
	<-s.stoppingChan
}

func (s *Server) Start() {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	stopping := false
	disconnectedClients := make(map[uuid.UUID]disconnectedClient)
	lastCheckedAt := time.Now()
	for !stopping {
		select {
		case <-time.After(10 * time.Minute):
		case <-s.stoppingChan:
			stopping = true
		case command := <-s.commands:
			switch command.t {
			case typeConnect:
				var clientId uuid.UUID
				var c *client
				if command.clientId != nil {
					if dc, ok := disconnectedClients[clientId]; ok {
						clientId = *command.clientId
						delete(disconnectedClients, clientId)
						c = &client{
							acks:   dc.client.acks,
							last:   dc.client.last,
							events: command.events,
						}
					}
				}
				if c == nil {
					clientId = uuid.New()
					c = &client{
						acks:   make(map[uint32]uint32),
						last:   make(map[uint32]uint32),
						events: command.events,
					}
				}
				s.clients[clientId] = c
				c.events <- Event{
					ConnectedClientId: &clientId,
				}
				event := Event{}
				for _, file := range s.files {
					event.Updates = append(event.Updates, file.Content())
				}
				c.events <- event
			case typeDisconnect:
				if c, ok := s.clients[*command.clientId]; ok {
					close(c.events)
					delete(s.clients, *command.clientId)
					disconnectedClients[*command.clientId] = disconnectedClient{
						client:         c,
						disconnectedAt: time.Now(),
					}
				}
			case typeCreateFiles:
				firstId, err := s.allocateFileIds(uint32(len(command.contents)))
				if err != nil {
					if s.ErrorProcessor != nil {
						s.ErrorProcessor(err)
					}
					break
				}
				fileIds := make([]uint32, len(command.contents))
				for i := 0; i < len(command.contents); i++ {
					fileId := firstId + uint32(i)
					s.files[fileId] = NewFile(fileId, command.contents[i])
					fileIds[i] = fileId
				}
				command.fileIdChan <- fileIds
				event := Event{
					CreatedFileIds: fileIds,
				}
				for _, c := range s.clients {
					c.events <- event
				}
			case typeCloseFiles:
				var err error
				for _, fileId := range command.fileIds {
					if _, ok := s.files[fileId]; !ok {
						err = fmt.Errorf("Cannot find file %d!", fileId)
						break
					}
				}
				if err != nil {
					if s.ErrorProcessor != nil {
						s.ErrorProcessor(err)
					}
					break
				}
				event := Event{}
				for _, fileId := range command.fileIds {
					event.ClosedFileIds = append(event.ClosedFileIds, fileId)
					delete(s.files, fileId)
					for _, c := range s.clients {
						delete(c.acks, fileId)
						delete(c.last, fileId)
					}
				}
				for _, c := range s.clients {
					c.events <- event
				}
			case typeContent:
				if file, ok := s.files[command.fileId]; ok {
					command.updates <- []ServerUpdate{file.Content()}
				} else {
					command.updates <- []ServerUpdate{}
				}
			case typeAllContents:
				contents := make([]ServerUpdate, 0)
				for _, file := range s.files {
					contents = append(contents, file.Content())
				}
				command.updates <- contents
			case typeSubmit:
				var err error
				var c *client
				if command.clientId != nil {
					c = s.clients[*command.clientId]
					if c == nil {
						err = fmt.Errorf("Unknown client: %s", *command.clientId)
					}
				}
				for _, change := range command.changes {
					if err != nil {
						continue
					}
					if c != nil && change.ClientVersion != c.last[change.Id]+1 {
						continue
					}
					if file, ok := s.files[change.Id]; ok {
						_, err = file.Submit(command.clientId, change)
						if err == nil && c != nil {
							c.last[change.Id] = change.ClientVersion
						}
					}
				}
				if err != nil {
					if s.ErrorProcessor != nil {
						s.ErrorProcessor(err)
					}
				} else {
					s.broadcast()
				}
			case typeAck:
				if c, ok := s.clients[*command.clientId]; ok {
					for id, version := range command.acks {
						c.acks[id] = version
					}
					s.broadcast()
				}
			case typeUpdate:
				if file, ok := s.files[command.fileId]; ok {
					content := file.Content()
					d, err := command.updateFunc(content.Delta)
					if err != nil {
						command.errorChan <- err
						break
					}
					_, err = file.Submit(nil, ClientChange{
						Id:    content.Id,
						Base:  content.Version,
						Delta: d,
					})
					command.errorChan <- err
				} else {
					command.errorChan <- fmt.Errorf("Cannot find file %d", command.fileId)
				}
			case typeUpdateAll:
				contents := make([]ServerUpdate, 0)
				for _, file := range s.files {
					contents = append(contents, file.Content())
				}
				changes, err := command.updateAllFunc(contents)
				if err != nil {
					command.errorChan <- err
					break
				}
				for _, change := range changes {
					if err != nil {
						continue
					}
					if file, ok := s.files[change.Id]; ok {
						_, err = file.Submit(nil, change)
					}
				}
				command.errorChan <- err
			case typeBroadcast:
				s.broadcast()
			case typeUndo:
				if file, ok := s.files[command.fileId]; ok {
					err := file.Undo()
					if err != nil {
						command.errorChan <- err
					} else {
						command.errorChan <- nil
						s.broadcast()
					}
				} else {
					command.errorChan <- fmt.Errorf("Cannot find file %d", command.fileId)
				}
			case typeRedo:
				if file, ok := s.files[command.fileId]; ok {
					err := file.Redo()
					if err != nil {
						command.errorChan <- err
					} else {
						command.errorChan <- nil
						s.broadcast()
					}
				} else {
					command.errorChan <- fmt.Errorf("Cannot find file %d", command.fileId)
				}
			}
		}
		now := time.Now()
		if now.After(lastCheckedAt.Add(10 * time.Minute)) {
			// Purge expired disconnected clients
			dcs := make(map[uuid.UUID]disconnectedClient)
			for clientId, dc := range disconnectedClients {
				if now.Before(dc.disconnectedAt.Add(time.Hour)) {
					dcs[clientId] = dc
				}
			}
			disconnectedClients = dcs
			lastCheckedAt = now
		}
	}
	// Close and remove all clients
	for _, c := range s.clients {
		close(c.events)
	}
	s.clients = make(map[uuid.UUID]*client)
	// Remove all files
	s.files = make(map[uint32]*File)
	atomic.CompareAndSwapInt32(&s.running, 1, 0)
	s.stoppingChan <- true
}

func (s *Server) allocateFileIds(num uint32) (uint32, error) {
	current := s.nextFileId
	for current != s.nextFileId-1 {
		found := true
		for i := uint32(0); i < num; i++ {
			if _, ok := s.files[current+i]; ok {
				found = false
				break
			}
		}
		if found {
			s.nextFileId = current + num
			return current, nil
		}
		current++
	}
	return 0, fmt.Errorf("Cannot allocate new file ID!")
}

func (s *Server) broadcast() {
	for clientId, c := range s.clients {
		event := Event{}
		for fileId, file := range s.files {
			change := file.UpdateSince(&clientId, c.acks[fileId])
			last := c.last[fileId]
			// TODO: we should also check last, when last is updated(it really is
			// server ack to the client), we should also broadcast the state.
			if change.Base != change.Version || last > 0 {
				change.LastCommittedClientVersion = &last
				event.Updates = append(event.Updates, change)
			}
		}
		if len(event.Updates) > 0 {
			c.events <- event
		}
	}
}
