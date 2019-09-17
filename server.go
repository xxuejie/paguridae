package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"nhooyr.io/websocket"
)

type File struct {
	Id             int
	LabelVersion   int
	Label          *delta.Delta
	ContentVersion int
	Content        *delta.Delta
}

func (f *File) LabelId() int {
	return f.Id
}

func (f *File) ContentId() int {
	return f.Id + 1
}

func (f *File) IdDelta() *delta.Delta {
	return delta.New(nil).Insert(fmt.Sprintf("%d\n%d\n", f.LabelId(), f.ContentId()), nil)
}

type Connection struct {
	Files []*File
}

func NewConnection() (*Connection, error) {
	currentPath, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	// A new connection has 2 files: an empty one, and one showing
	// contents from current directory
	cmd := exec.Command("ls", "-F", currentPath)
	var out strings.Builder
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	file1 := &File{
		Id:             1,
		LabelVersion:   1,
		Label:          delta.New(nil).Insert(" | New Newcol Cut Copy Paste Put", nil),
		ContentVersion: 1,
		Content:        delta.New(nil),
	}
	file2 := &File{
		Id:             3,
		LabelVersion:   1,
		Label:          delta.New(nil).Insert(currentPath, nil).Insert(" | New Newcol Cut Copy Paste Put", nil),
		ContentVersion: 1,
		Content:        delta.New(nil).Insert(out.String(), nil),
	}
	connection := &Connection{
		Files: []*File{
			file1, file2,
		},
	}
	return connection, nil
}

type Change struct {
	Id      int         `json:"id"`
	Change  delta.Delta `json:"change"`
	Version int         `json:"version"`
}

type Action struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Index  int    `json:"index"`
	Length int    `json:"length"`
}

type Request struct {
	Changes []Change `json:"changes"`
	Action  Action   `json:"action"`
}

type Ack struct {
	Id         int `json:"id"`
	AckVersion int `json:"ack_version"`
	Version    int `json:"version"`
}

type Update struct {
	Changes []Change `json:"changes"`
	Acks    []Ack    `json:"acks"`
}

func (c *Connection) Serve(ctx context.Context, socketConn *websocket.Conn) error {
	idDelta := delta.New(nil)
	for _, file := range c.Files {
		idDelta = idDelta.Concat(*file.IdDelta())
	}
	changes := make([]Change, 0)
	changes = append(changes, Change{
		Id:      0,
		Version: 1,
		Change:  *idDelta,
	})
	for _, file := range c.Files {
		changes = append(changes, Change{
			Id:      file.LabelId(),
			Version: file.LabelVersion,
			Change:  *file.Label,
		})
		changes = append(changes, Change{
			Id:      file.ContentId(),
			Version: file.ContentVersion,
			Change:  *file.Content,
		})
	}
	updateBytes, err := json.Marshal(Update{
		Changes: changes,
		Acks:    []Ack{},
	})
	if err != nil {
		return err
	}
	err = socketConn.Write(ctx, websocket.MessageText, updateBytes)
	if err != nil {
		return err
	}

	for {
		_, b, err := socketConn.Read(ctx)
		if err != nil {
			return err
		}
		// log.Print("Message: ", string(b))
		var request Request
		err = json.Unmarshal(b, &request)
		if err != nil {
			log.Print("Error unmarshaling message:", err)
			continue
		}

		acks := c.applyChanges(request.Changes)
		// TODO: handle action
		log.Print("Request action:", request.Action)

		updateBytes, err := json.Marshal(Update{
			Changes: []Change{},
			Acks:    acks,
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

func (c *Connection) applyChanges(changes []Change) []Ack {
	acks := make([]Ack, 0)
	for _, change := range changes {
		for _, file := range c.Files {
			version := 0
			// TODO: version testing and OT transform when needed,
			// right now we just simply deny unmatched versions.
			if file.LabelId() == change.Id {
				if change.Version != file.LabelVersion {
					log.Println("Invalid file version!")
					continue
				}
				file.Label = file.Label.Compose(change.Change)
				file.LabelVersion += 1
				version = file.LabelVersion
			}
			if file.ContentId() == change.Id {
				if change.Version != file.ContentVersion {
					log.Println("Invalid file version!")
					continue
				}
				file.Content = file.Content.Compose(change.Change)
				file.ContentVersion += 1
				version = file.ContentVersion
			}
			if version != 0 {
				acks = append(acks, Ack{
					Id:         change.Id,
					Version:    version,
					AckVersion: change.Version,
				})
				break
			}
		}
	}
	return acks
}
