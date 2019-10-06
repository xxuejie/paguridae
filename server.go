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

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"nhooyr.io/websocket"
)

const (
	DefaultLabel = " | New Del Put"
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

func (f *File) Path() string {
	return strings.SplitN(DeltaToString(*f.Label), " ", 2)[0]
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

func (f *File) FullDeltas() []Change {
	return []Change{
		Change{
			Id:      f.LabelId(),
			Version: f.LabelVersion,
			Delta:   *f.Label,
		},
		Change{
			Id:      f.ContentId(),
			Version: f.ContentVersion,
			Delta:   *f.Content,
		},
	}
}

func NewDummyFile(id int) File {
	return File{
		Id:             id,
		LabelVersion:   1,
		Label:          delta.New(nil).Insert(DefaultLabel, nil),
		ContentVersion: 1,
		Content:        delta.New(nil),
	}
}

func NewOpenedFile(id int, path string) (*File, error) {
	// TODO: partial read for larger files
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if stat.Size() > 128*1024 {
		return nil, errors.New("File too large!")
	}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &File{
		Id:             id,
		LabelVersion:   1,
		Label:          delta.New(nil).Insert(fmt.Sprintf("%s%s", path, DefaultLabel), nil),
		ContentVersion: 1,
		Content:        delta.New(nil).Insert(string(content), nil),
	}, nil
}

func NewDirectoryFile(id int, path string) (*File, error) {
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	cmd := exec.Command("ls", "-F", path)
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	return &File{
		Id:             id,
		LabelVersion:   1,
		Label:          delta.New(nil).Insert(fmt.Sprintf("%s%s", path, DefaultLabel), nil),
		ContentVersion: 1,
		Content:        delta.New(nil).Insert(out.String(), nil),
	}, nil
}

type command struct {
	action    Action
	fileIndex int
	file      *File
}

type Connection struct {
	Files          []*File
	IdDeltaVersion int
	NextId         int
}

func (c *Connection) nextId() int {
	i := c.NextId
	c.NextId += 2
	return i
}

func NewConnection() *Connection {
	return &Connection{
		Files:          make([]*File, 0),
		IdDeltaVersion: 0,
		NextId:         1,
	}
}

func (c *Connection) Serve(ctx context.Context, socketConn *websocket.Conn) error {
	// A new connection has 2 files: an empty one, and one showing
	// contents from current directory
	file1 := NewDummyFile(c.nextId())
	currentPath, err := os.Getwd()
	if err != nil {
		return err
	}
	file2, err := NewDirectoryFile(c.nextId(), currentPath)
	if err != nil {
		return err
	}
	changes := make([]Change, 0)
	changes = append(changes, c.appendFiles(&file1, file2))
	for _, file := range c.Files {
		changes = append(changes, file.FullDeltas()...)
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

		var changes = make([]Change, 0)
		acks := c.applyChanges(request.Changes)
		fileIndex, file := c.findFile(request.Action)
		if file != nil {
			executeChanges, err := c.execute(command{
				action:    request.Action,
				fileIndex: fileIndex,
				file:      file,
			})
			if err != nil {
				log.Print("Error executing command:", err)
			} else {
				changes = append(changes, executeChanges...)
			}
		} else {
			log.Print("Cannot find file for action: ", request.Action.Id)
		}

		updateBytes, err := json.Marshal(Update{
			Changes: changes,
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

func (c *Connection) appendFiles(files ...*File) Change {
	oldDelta := c.idDelta()
	c.Files = append(c.Files, files...)
	newDelta := c.idDelta()
	d := Diff(*oldDelta, *newDelta)
	c.IdDeltaVersion += 1
	return Change{
		Id:      0,
		Version: c.IdDeltaVersion,
		Delta:   *d,
	}
}

func (c *Connection) deleteFile(fileIndex int) Change {
	oldDelta := c.idDelta()
	l := len(c.Files)
	c.Files[fileIndex] = c.Files[l-1]
	c.Files[l-1] = nil
	c.Files = c.Files[:l-1]
	newDelta := c.idDelta()
	d := Diff(*oldDelta, *newDelta)
	c.IdDeltaVersion += 1
	return Change{
		Id:      0,
		Version: c.IdDeltaVersion,
		Delta:   *d,
	}
}

func (c *Connection) idDelta() *delta.Delta {
	d := delta.New(nil)
	for _, file := range c.Files {
		d = d.Concat(*file.IdDelta())
	}
	return d
}

func (c *Connection) findFile(action Action) (int, *File) {
	for i, file := range c.Files {
		if action.FileId() == file.Id {
			return i, file
		}
	}
	return -1, nil
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

func (c *Connection) execute(command command) ([]Change, error) {
	if command.action.Action == "search" {
		path := command.file.Path()
		if !strings.HasSuffix(path, "/") {
			path += "/../"
		}
		path += command.action.Selection
		path = filepath.Clean(path)
		stat, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			} else {
				return nil, err
			}
		}
		var f *File
		if stat.IsDir() {
			f, err = NewDirectoryFile(c.nextId(), path)
		} else {
			f, err = NewOpenedFile(c.nextId(), path)
		}
		if err != nil {
			return nil, err
		}
		changes := make([]Change, 0)
		changes = append(changes, c.appendFiles(f))
		changes = append(changes, f.FullDeltas()...)
		return changes, nil
	} else if command.action.Action == "execute" {
		switch command.action.Selection {
		case "New":
			f := NewDummyFile(c.nextId())
			changes := make([]Change, 0)
			changes = append(changes, c.appendFiles(&f))
			changes = append(changes, f.FullDeltas()...)
			return changes, nil
		case "Del":
			return []Change{c.deleteFile(command.fileIndex)}, nil
		default:
			return nil, errors.New(fmt.Sprint("Unknown execute command:", command.action.Selection))
		}
	} else {
		return nil, errors.New(fmt.Sprint("Unknown command:", command))
	}
}
