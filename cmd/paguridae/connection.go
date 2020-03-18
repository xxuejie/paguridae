package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/xxuejie/go-delta-ot/ot"
	"nhooyr.io/websocket"
)

const (
	DefaultLabel   = " | New Del Put"
	UserClientId   = 0
	SystemClientId = 1
	MetaFileId     = 0
)

func idToMeta(id uint32) delta.Delta {
	return *delta.New(nil).Insert(fmt.Sprintf("%d\n", id), nil)
}

func extractPath(d delta.Delta) string {
	return strings.SplitN(DeltaToString(d), " ", 2)[0]
}

type Connection struct {
	BufferedChanges []ot.MultiFileChange
	NextId          uint32
	Server          *ot.MultiFileServer

	mux sync.Mutex
}

func NewConnection() (*Connection, error) {
	server := ot.NewMultiFileServer()
	go func() {
		server.Start()
	}()
	connection := &Connection{
		BufferedChanges: make([]ot.MultiFileChange, 0),
		NextId:          MetaFileId + 1,
		Server:          server,
	}
	userCreated, userUpdates := server.NewClient(UserClientId)
	if !(<-userCreated) {
		server.Stop()
		return nil, errors.New("Error creating user client!")
	}
	go func() {
		for change := range userUpdates {
			connection.AddChanges(change)
		}
	}()
	systemCreated, systemUpdates := server.NewClient(SystemClientId)
	if !(<-systemCreated) {
		server.Stop()
		return nil, errors.New("Error creating system client!")
	}
	go func() {
		for range systemUpdates {
		}
	}()
	if !(<-server.NewFile(MetaFileId, *delta.New(nil))) {
		server.Stop()
		return nil, errors.New("Error creating metafile!")
	}
	return connection, nil
}

func (c *Connection) Stop() {
	c.Server.Stop()
}

func (c *Connection) AddChanges(changes ...ot.MultiFileChange) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.BufferedChanges = append(c.BufferedChanges, changes...)
}

func (c *Connection) GrabChanges() []ot.MultiFileChange {
	c.mux.Lock()
	defer c.mux.Unlock()

	changes := c.BufferedChanges
	c.BufferedChanges = make([]ot.MultiFileChange, 0)
	return changes
}

func (c *Connection) refreshMetafile(newIds ...uint32) {
	oldMeta := c.Server.CurrentChange(MetaFileId)
	if oldMeta == nil || oldMeta.Change.Delta == nil {
		log.Printf("Metafile does not exist, something is seriously wrong!")
		return
	}
	newMetaContent := delta.New(nil)
	for _, change := range c.Server.AllChanges() {
		if change.Id != MetaFileId {
			newMetaContent = newMetaContent.Concat(idToMeta(change.Id))
		}
	}
	for _, newId := range newIds {
		newMetaContent = newMetaContent.Concat(idToMeta(newId))
	}
	d := Diff(*oldMeta.Change.Delta, *newMetaContent)
	c.Server.Submit(SystemClientId, ot.MultiFileChange{
		Id: MetaFileId,
		Change: ot.Change{
			Delta:   d,
			Version: oldMeta.Change.Version,
		},
	})
}

func (c *Connection) closeFile(fileId uint32) {
	notify := make(chan bool)
	c.Server.CloseFile(fileId, notify)
	<-notify
}

func (c *Connection) createFile(label string, content *string) error {
	labelId := c.NextId
	contentId := c.NextId + 1
	c.NextId += 2
	c.refreshMetafile(labelId, contentId)
	if !(<-c.Server.NewFile(labelId, *delta.New(nil).Insert(label, nil))) {
		c.refreshMetafile()
		return errors.New("Error creating label component!")
	}
	contentDelta := delta.New(nil)
	if content != nil {
		contentDelta = contentDelta.Insert(*content, nil)
	}
	if !(<-c.Server.NewFile(contentId, *contentDelta)) {
		c.closeFile(labelId)
		c.refreshMetafile()
		return errors.New("Error creating content component!")
	}
	return nil
}

func (c *Connection) CreateDummyFile() error {
	return c.createFile(DefaultLabel, nil)
}

func (c *Connection) CreateDirectoryListingFile(path string) error {
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	cmd := exec.Command("ls", "-F", path)
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return err
	}
	content := out.String()
	return c.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), &content)
}

func (c *Connection) FindOrOpenFile(path string) error {
	// TODO: skip opening already opened file
	// TODO: partial read for larger files
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.Size() > 128*1024 {
		return errors.New("File too large!")
	}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	contentString := string(content)
	return c.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), &contentString)
}

func (c *Connection) Serve(ctx context.Context, socketConn *websocket.Conn) error {
	// A new connection has 2 files: an empty one, and one showing
	// contents from current directory
	currentPath, err := os.Getwd()
	if err != nil {
		return err
	}
	err = c.CreateDummyFile()
	if err != nil {
		return err
	}
	err = c.CreateDirectoryListingFile(currentPath)
	if err != nil {
		return err
	}

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
	for {
		select {
		case b := <-messageChan:
			var request Request
			err = json.Unmarshal(b, &request)
			if err != nil {
				log.Print("Error unmarshaling message:", err)
				continue
			}

			c.applyChanges(request.Changes)
			err = c.execute(request.Action)
			if err != nil {
				log.Print("Error executing action:", err)
			}
			timeout = 10 * time.Millisecond
		case err := <-errorChan:
			return err
		case <-time.After(timeout):
			timeout = timeout * 2
		}

		changes := c.GrabChanges()
		if len(changes) > 0 {
			updateBytes, err := json.Marshal(Update{
				Changes: changes,
			})
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

func (c *Connection) deleteFile(action Action) {
	c.closeFile(action.LabelId())
	c.closeFile(action.ContentId())
	c.refreshMetafile()
}

func (c *Connection) applyChanges(changes []ot.MultiFileChange) {
	for _, change := range changes {
		c.Server.Submit(UserClientId, change)
	}
}

func (c *Connection) execute(action Action) error {
	if action.Type == "search" {
		var path string
		if strings.HasPrefix(action.Selection, "/") {
			path = action.Selection
		} else {
			labelContent := c.Server.CurrentChange(action.LabelId())
			if labelContent == nil {
				return fmt.Errorf("Cannot find label file: %d, something must be wrong", action.LabelId())
			}
			if labelContent.Change.Delta != nil {
				path = extractPath(*labelContent.Change.Delta)
			}
			if !strings.HasSuffix(path, "/") {
				path += "/../"
			}
			path += action.Selection
		}
		path = filepath.Clean(path)
		stat, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			} else {
				return err
			}
		}
		if stat.IsDir() {
			err = c.CreateDirectoryListingFile(path)
		} else {
			err = c.FindOrOpenFile(path)
		}
		if err != nil {
			return err
		}
		return nil
	} else if action.Type == "execute" {
		switch action.Selection {
		case "New":
			return c.CreateDummyFile()
		case "Del":
			c.deleteFile(action)
			return nil
		default:
			return errors.New(fmt.Sprint("Unknown execute command:", action.Selection))
		}
	} else {
		return errors.New(fmt.Sprint("Unknown action type:", action.Type))
	}
}
