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

const (
	ExtractSelectionLength = 256
	DefaultLabel = " | New Newcol Cut Copy Paste Put"
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

func NewDummyFile(id int) File {
	return File{
		Id: id,
		LabelVersion: 1,
		Label: delta.New(nil).Insert(DefaultLabel, nil),
		ContentVersion: 1,
		Content: delta.New(nil),
	}
}

func NewDirectoryFile(id int, path string) (*File, error) {
	cmd := exec.Command("ls", "-F", path)
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	return &File{
		Id: id,
		LabelVersion: 1,
		Label: delta.New(nil).Insert(fmt.Sprintf("%s%s", path, DefaultLabel), nil),
		ContentVersion: 1,
		Content: delta.New(nil).Insert(out.String(), nil),
	}, nil
}

type Connection struct {
	Files []*File
	NextId int
}

func (c *Connection) nextId() int {
	i := c.NextId
	c.NextId += 2
	return i
}

func NewConnection() (*Connection, error) {
	c := &Connection{
		Files: make([]*File, 0),
		NextId: 1,
	}
	// A new connection has 2 files: an empty one, and one showing
	// contents from current directory
	file1 := NewDummyFile(c.nextId())
	currentPath, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	file2, err := NewDirectoryFile(c.nextId(), currentPath)
	if err != nil {
		return nil, err
	}
	c.Files = append(c.Files, &file1, file2)
	return c, nil
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
