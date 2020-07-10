package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"xuejie.space/c/paguridae/pkg/ot"
)

type Connection struct {
	id              uuid.UUID
	session         *Session
	flushChan       <-chan bool
	bufferedUpdates map[uint32]ot.ServerUpdate
	mux             sync.Mutex
}

func NewConnection(clientId *uuid.UUID, session *Session) *Connection {
	id, userEvents, flushChan := session.Connect(clientId)

	connection := &Connection{
		id:              id,
		session:         session,
		flushChan:       flushChan,
		bufferedUpdates: make(map[uint32]ot.ServerUpdate),
	}
	go func(c *Connection) {
		for event := range userEvents {
			if len(event.Updates) > 0 {
				c.AddUpdates(event.Updates...)
			}
		}
	}(connection)
	return connection
}

func (c *Connection) Id() uuid.UUID {
	return c.id
}

func (c *Connection) Disconnect() {
	c.session.Disconnect(c.id)
}

func (c *Connection) AddUpdates(updates ...ot.ServerUpdate) {
	c.mux.Lock()
	defer c.mux.Unlock()

	for _, update := range updates {
		c.bufferedUpdates[update.Id] = update
	}
}

func (c *Connection) GrabUpdates() map[uint32]ot.ServerUpdate {
	c.mux.Lock()
	defer c.mux.Unlock()

	updates := c.bufferedUpdates
	c.bufferedUpdates = make(map[uint32]ot.ServerUpdate)
	return updates
}

func (c *Connection) Serve(ctx context.Context, socketConn *websocket.Conn) error {
	log.Printf("Serving connection %s", c.id)
	messageChan := make(chan []byte)
	errorChan := make(chan error)
	go func() {
		for {
			_, b, err := socketConn.Read(ctx)
			if err != nil {
				errorChan <- err
				return
			}
			messageChan <- b
		}
	}()

	timeout := 10 * time.Millisecond
	var selection *Selection
	var selectionCreated bool
	for {
		select {
		case b := <-messageChan:
			var request Request
			err := json.Unmarshal(b, &request)
			if err != nil {
				log.Print("Error unmarshaling message:", err)
				continue
			}

			c.session.Server.Acks(c.id, request.Acks)
			err = c.session.ApplyChanges(c.id, request.Changes)
			if err != nil {
				log.Print("Error applying changes:", err)
			}
			if request.Action != nil {
				aSelection, aSelectionCreated, err := c.session.Execute(c.id, *request.Action)
				if err != nil {
					log.Print("Error executing action:", err)
				} else if aSelection != nil {
					selection, selectionCreated = aSelection, aSelectionCreated
				}
			}
			timeout = 10 * time.Millisecond
		case <-c.flushChan:
			timeout = 10 * time.Millisecond
		case err := <-errorChan:
			return err
		case <-time.After(timeout):
			timeout = timeout * 2
		}

		updates := c.GrabUpdates()
		if len(updates) > 0 || selection != nil {
			var hashes map[uint32]string
			if c.session.VerifyContent {
				hashes = make(map[uint32]string)
				for _, update := range updates {
					if _, ok := hashes[update.Id]; !ok {
						latestContent := *c.session.Server.Content(update.Id)
						if latestContent.Version == update.Version {
							content := DeltaToString(latestContent.Delta)
							// QuillJS always add a new line at the very end of editor
							if update.Id != MetaFileId {
								content += "\n"
							}
							hashes[update.Id] = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
						}
					}
				}
			}

			updateData := Update{
				Updates: updates,
				Hashes:  hashes,
			}
			if selection != nil {
				_, ok := updateData.Updates[selection.Id]
				if (!selectionCreated) || ok {
					updateData.Selection = selection
					selection = nil
				}
			}
			updateBytes, err := json.Marshal(updateData)
			if err != nil {
				return err
			}
			err = socketConn.Write(ctx, websocket.MessageText, updateBytes)
			if err != nil {
				return err
			}
		}
	}
}
