package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
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

const (
	ExtractSelectionLength = 256
)

func (f *File) ExtractSelection(action Action) string {
	var delta *delta.Delta
	if action.Id == f.LabelId() {
		delta = f.Label
	} else {
		delta = f.Content
	}
	if action.Length > 0 {
		return DeltaToString(*delta.Slice(action.Index, action.Index+action.Length))
	} else {
		start := action.Index - ExtractSelectionLength
		if start < 0 {
			start = 0
		}
		firstHalf := DeltaToString(*delta.Slice(start, action.Index))
		secondHalf := DeltaToString(*delta.Slice(action.Index, action.Index+ExtractSelectionLength))
		firstHalfMatch := regexp.MustCompile(`\S+$`).FindString(firstHalf)
		secondHalfMatch := regexp.MustCompile(`^\S+`).FindString(secondHalf)
		return strings.Join([]string{firstHalfMatch, secondHalfMatch}, "")
	}
}

func (f *File) ApplyChange(change Change) (*Ack, error) {
	version := 0
	if change.Id == f.LabelId() {
		// TODO: version testing and OT transform when needed,
		// right now we just simply deny unmatched versions.
		if change.Version != f.LabelVersion {
			return nil, errors.New("Invalid file label version!")
		}
		f.Label = f.Label.Compose(change.Delta)
		f.LabelVersion += 1
		version = f.LabelVersion
	} else {
		if change.Version != f.ContentVersion {
			return nil, errors.New("Invalid file content version!")
		}
		f.Content = f.Content.Compose(change.Delta)
		f.ContentVersion += 1
		version = f.ContentVersion
	}
	return &Ack{
		Id:      change.Id,
		Version: version,
	}, nil
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
	Delta   delta.Delta `json:"delta"`
	Version int         `json:"version"`
}

func (c Change) FileId() int {
	return c.Id - 1 + c.Id%2
}

type Action struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Index  int    `json:"index"`
	Length int    `json:"length"`
}

func (a Action) FileId() int {
	return a.Id - 1 + a.Id%2
}

type Request struct {
	Changes []Change `json:"changes"`
	Action  Action   `json:"action"`
}

type Ack struct {
	Id int `json:"id"`
	// New version
	Version int `json:"version"`
}

type Update struct {
	Changes []Change `json:"changes,omitempty"`
	Acks    []Ack    `json:"acks,omitempty"`
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
		Delta:   *idDelta,
	})
	for _, file := range c.Files {
		changes = append(changes, Change{
			Id:      file.LabelId(),
			Version: file.LabelVersion,
			Delta:   *file.Label,
		})
		changes = append(changes, Change{
			Id:      file.ContentId(),
			Version: file.ContentVersion,
			Delta:   *file.Content,
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
		file := c.findFile(request.Action)
		if file != nil {
			selection := file.ExtractSelection(request.Action)
			log.Println("Action: ", request.Action.Action,
				" selection: ", string(selection),
				" file ID: ", request.Action.FileId())
		} else {
			log.Print("Cannot find file for action: ", request.Action.Id)
		}

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

func (c *Connection) findFile(action Action) *File {
	for _, file := range c.Files {
		if action.FileId() == file.Id {
			return file
		}
	}
	return nil
}

func (c *Connection) applyChanges(changes []Change) []Ack {
	acks := make([]Ack, 0)
	for _, change := range changes {
		for _, file := range c.Files {
			if change.FileId() == file.Id {
				ack, err := file.ApplyChange(change)
				if err != nil {
					log.Println("Applying change error: ", err)
				} else {
					acks = append(acks, *ack)
				}
				break
			}
		}
	}
	return acks
}
