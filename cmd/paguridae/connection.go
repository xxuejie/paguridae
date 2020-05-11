package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/google/uuid"
	"github.com/xxuejie/go-delta-ot/ot"
	"nhooyr.io/websocket"
	"xuejie.space/c/go-quill-editor"
)

const (
	DefaultLabel          = " | New Del Put"
	UserClientId          = 0
	SystemClientId        = 1
	MetaFileId            = 0
	CommandTimeoutSeconds = 10
)

func extractPath(d delta.Delta) string {
	return strings.SplitN(DeltaToString(d), " ", 2)[0]
}

type Connection struct {
	Id              string
	BufferedChanges []ot.MultiFileChange
	NextId          uint32
	Server          *ot.MultiFileServer
	VerifyContent   bool
	Flush           chan bool

	listenPath     string
	listener       net.Listener
	listenerSignal chan bool
	mux            sync.Mutex
}

func NewConnection(verifyContent bool) (*Connection, error) {
	id := uuid.New().String()
	listenPath := fmt.Sprintf("/tmp/paguridae/%s", id)
	listenDirectory := filepath.Dir(listenPath)
	_, err := os.Stat(listenDirectory)
	if os.IsNotExist(err) {
		err = os.Mkdir(listenDirectory, 0755)
	}
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", listenPath)
	if err != nil {
		return nil, err
	}
	server := ot.NewMultiFileServer()
	go func() {
		server.Start()
	}()
	connection := &Connection{
		Id:              id,
		BufferedChanges: make([]ot.MultiFileChange, 0),
		NextId:          MetaFileId + 1,
		Server:          server,
		VerifyContent:   verifyContent,
		Flush:           make(chan bool),
		listenPath:      listenPath,
		listener:        listener,
		listenerSignal:  make(chan bool),
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
	// A new connection has 2 files: an empty one, and one showing
	// contents from current directory
	currentPath, err := os.Getwd()
	if err != nil {
		server.Stop()
		return nil, err
	}
	_, err = connection.CreateDummyFile()
	if err != nil {
		server.Stop()
		return nil, err
	}
	err = connection.CreateDirectoryListingFile(currentPath)
	if err != nil {
		server.Stop()
		return nil, err
	}
	err = Start9PFileSystem(connection)
	if err != nil {
		server.Stop()
		return nil, err
	}
	return connection, nil
}

func (c *Connection) Stop() {
	close(c.listenerSignal)
	c.listener.Close()
	os.Remove(c.listenPath)
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

func idToMeta(id uint32) delta.Delta {
	return *delta.New(nil).Insert(fmt.Sprintf("%d 0 0\n", id), nil)
}

func (c *Connection) refreshMetafile(newIds ...uint32) {
	c.Server.FetchAndChangeAll(SystemClientId, func(changes []ot.MultiFileChange) []ot.MultiFileChange {
		var oldMeta *ot.MultiFileChange
		for _, change := range changes {
			if change.Id == MetaFileId {
				oldMeta = &change
				break
			}
		}
		if oldMeta == nil || oldMeta.Change.Delta == nil {
			log.Printf("Metafile does not exist, something is seriously wrong!")
			return []ot.MultiFileChange{}
		}
		oldInfos := make(map[uint32]string)
		for _, line := range strings.Split(DeltaToString(*oldMeta.Change.Delta), "\n") {
			if len(line) == 0 {
				continue
			}
			pieces := strings.SplitN(line, " ", 2)
			if len(pieces) != 2 {
				log.Printf("Invalid split for line: %s", line)
				return []ot.MultiFileChange{}
			}
			i, err := strconv.ParseUint(pieces[0], 10, 32)
			if err != nil {
				log.Printf("Unexpected parse error: %v", err)
				return []ot.MultiFileChange{}
			}
			oldInfos[uint32(i)] = line
		}
		newMetaContent := delta.New(nil)
		for _, change := range changes {
			if change.Id != MetaFileId {
				line, ok := oldInfos[change.Id]
				if ok {
					newMetaContent = newMetaContent.Concat(*delta.New(nil).Insert(line, nil).Insert("\n", nil))
				} else {
					newMetaContent = newMetaContent.Concat(idToMeta(change.Id))
				}
			}
		}
		for _, newId := range newIds {
			newMetaContent = newMetaContent.Concat(idToMeta(newId))
		}
		d := Diff(*oldMeta.Change.Delta, *newMetaContent)
		return []ot.MultiFileChange{
			{
				Id: MetaFileId,
				Change: ot.Change{
					Delta:   d,
					Version: oldMeta.Change.Version,
				},
			},
		}
	})
}

func (c *Connection) closeFile(fileId uint32) {
	notify := make(chan bool)
	c.Server.CloseFile(fileId, notify)
	<-notify
}

func (c *Connection) createFile(label string, content *string) (uint32, error) {
	labelId := c.NextId
	contentId := c.NextId + 1
	c.NextId += 2
	c.refreshMetafile(labelId, contentId)
	if !(<-c.Server.NewFile(labelId, *delta.New(nil).Insert(label, nil))) {
		c.refreshMetafile()
		return 0, errors.New("Error creating label component!")
	}
	contentDelta := delta.New(nil)
	if content != nil {
		contentDelta = contentDelta.Insert(*content, nil)
	}
	if !(<-c.Server.NewFile(contentId, *contentDelta)) {
		c.closeFile(labelId)
		c.refreshMetafile()
		return 0, errors.New("Error creating content component!")
	}
	return contentId, nil
}

func (c *Connection) CreateDummyFile() (uint32, error) {
	return c.createFile(DefaultLabel, nil)
}

func (c *Connection) FindOrCreateDummyFile(path string) (uint32, error) {
	for _, change := range c.Server.AllChanges() {
		if change.Id%2 != 0 {
			if extractPath(*change.Change.Delta) == path {
				// Return content Id based on current label Id
				return change.Id + 1, nil
			}
		}
	}
	return c.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), nil)
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
	_, err = c.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), &content)
	return err
}

func (c *Connection) FindOrOpenFile(path string) (uint32, error) {
	// TODO: partial read for larger files
	stat, err := os.Stat(path)
	for _, change := range c.Server.AllChanges() {
		if change.Id%2 != 0 {
			if extractPath(*change.Change.Delta) == path {
				// Return content Id based on current label Id
				return change.Id + 1, nil
			}
		}
	}
	if err != nil {
		return 0, err
	}
	if stat.Size() > 128*1024 {
		return 0, errors.New("File too large!")
	}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	contentString := string(content)
	return c.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), &contentString)
}

func (c *Connection) Serve(ctx context.Context, socketConn *websocket.Conn) error {
	log.Printf("Serving connection %s", c.Id)
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
			err := json.Unmarshal(b, &request)
			if err != nil {
				log.Print("Error unmarshaling message:", err)
				continue
			}

			err = c.applyChanges(request.Changes)
			if err != nil {
				log.Print("Error applying changes:", err)
			}
			err = c.execute(request.Action)
			if err != nil {
				log.Print("Error executing action:", err)
			}
			timeout = 10 * time.Millisecond
		case err := <-errorChan:
			return err
		case <-c.Flush:
			timeout = 10 * time.Millisecond
		case <-time.After(timeout):
			timeout = timeout * 2
		}

		changes := c.GrabChanges()
		if len(changes) > 0 {
			var hashes map[uint32]string
			if c.VerifyContent {
				hashes = make(map[uint32]string)
				for _, change := range changes {
					if _, ok := hashes[change.Id]; !ok {
						latestContent := *c.Server.CurrentChange(change.Id)
						if latestContent.Change.Version == change.Change.Version {
							content := DeltaToString(*latestContent.Change.Delta)
							// QuillJS always add a new line at the very end of editor
							if change.Id != MetaFileId {
								content += "\n"
							}
							hashes[change.Id] = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
						}
					}
				}
			}

			updateBytes, err := json.Marshal(Update{
				Changes: changes,
				Hashes:  hashes,
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

func (c *Connection) editFile(action Action) {
	var errorBuffer bytes.Buffer
	completeChan := make(chan bool, 1)
	c.Server.FetchAndChange(SystemClientId, action.Selection.Id, func(d delta.Delta) delta.Delta {
		f := editor.NewDeltaFile(d)
		f.Select(int64(action.Selection.Range.Index),
			int64(action.Selection.Range.Index+action.Selection.Range.Length))
		cmdStr := action.Command[4:]
		cmd, err := editor.Compile(cmdStr)
		if err != nil {
			return *delta.New(nil)
		}
		err = cmd.Run(editor.Context{
			File:    f,
			Printer: &errorBuffer,
		})
		completeChan <- (err == nil)
		if err != nil {
			return *delta.New(nil)
		}
		return f.Changes()
	})
	if <-completeChan && errorBuffer.Len() > 0 {
		c.newErrorBuffer(action).Write(errorBuffer.Bytes())
	}
}

func (c *Connection) newErrorBuffer(action Action) *errorsBufferWriter {
	return &errorsBufferWriter{
		path: filepath.Dir(extractPath(
			*c.Server.CurrentChange(action.LabelId()).Change.Delta)),
		c: c,
	}
}

func (c *Connection) runSamCommand(fileId uint32, cmd string) error {
	compiledCmd, err := editor.Compile(cmd)
	if err != nil {
		return err
	}
	errChan := make(chan error, 1)
	c.Server.FetchAndChange(SystemClientId, fileId, func(d delta.Delta) delta.Delta {
		f := editor.NewDeltaFile(d)
		errChan <- compiledCmd.Run(editor.Context{
			File: f,
		})
		return f.Changes()
	})
	return <-errChan
}

func (c *Connection) applyChanges(changes []ot.MultiFileChange) error {
	for _, change := range changes {
		if change.Change.Delta == nil {
			log.Printf("Ack changes submitted by client, this should not happen!")
		}
		c.Server.Submit(UserClientId, change)
		if change.Id > 0 {
			err := c.runSamCommand(change.Id-1, `1s/\|/|*/`)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Connection) execute(action Action) error {
	var labelPath string
	labelContent := c.Server.CurrentChange(action.LabelId())
	if labelContent == nil {
		return fmt.Errorf("Cannot find label file: %d, something must be wrong", action.LabelId())
	}
	if labelContent.Change.Delta != nil {
		labelPath = extractPath(*labelContent.Change.Delta)
	}

	if action.Type == "search" {
		var path string
		if strings.HasPrefix(action.Command, "/") {
			path = action.Command
		} else {
			path = labelPath
			if !strings.HasSuffix(path, "/") {
				path += "/../"
			}
			path += action.Command
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
			_, err = c.FindOrOpenFile(path)
		}
		if err != nil {
			return err
		}
		return nil
	} else if action.Type == "execute" {
		switch action.Command {
		case "New":
			_, err := c.CreateDummyFile()
			return err
		case "Del":
			c.deleteFile(action)
			return nil
		case "Put":
			// Put command here ignores all embeds and just save texts to a file, later
			// we can add a different command that do saves embeds in the buffer
			if action.Id == MetaFileId || len(labelPath) == 0 {
				return nil
			}
			fileContent := c.Server.CurrentChange(action.ContentId())
			var data []byte
			if fileContent != nil && fileContent.Change.Delta != nil {
				data = []byte(DeltaToString(*fileContent.Change.Delta))
			}
			err := ioutil.WriteFile(labelPath, data, 0755)
			if err != nil {
				return err
			}
			return c.runSamCommand(action.LabelId(), `1s/\|\*/|/`)
		default:
			if strings.HasPrefix(action.Command, "Edit") {
				c.editFile(action)
				return nil
			}
			cmds := strings.Split(strings.TrimSpace(action.Command), " ")
			if len(cmds) > 0 && len(cmds[0]) > 0 {
				firstChar := string(cmds[0][0])
				pipeSelectionToStdin := firstChar == "|" || firstChar == ">"
				pipeStdoutToSelection := firstChar == "|" || firstChar == "<"
				if pipeSelectionToStdin || pipeStdoutToSelection {
					cmds[0] = cmds[0][1:]
				}
				path, err := exec.LookPath(cmds[0])
				if err == nil {
					var cancelCmd context.CancelFunc
					ctx := context.Background()
					if pipeStdoutToSelection {
						ctx, cancelCmd = context.WithTimeout(ctx, CommandTimeoutSeconds*time.Second)
					}
					cmd := exec.CommandContext(ctx, path, cmds[1:]...)
					// acmeaddr is different from paguridaesaddr. acmeaddr describes the command
					// argument sent via mouse chording, while paguridaesaddr describes the addr
					// for selected texts passed in via pipes. Later if we decide to add mouse
					// chording, we can then include acmeaddr here.
					cmd.Env = append(os.Environ(),
						fmt.Sprintf("winid=%d", action.Id),
						fmt.Sprintf("%%=%s", labelPath),
						fmt.Sprintf("samfile=%s", labelPath),
						fmt.Sprintf("paguridaesid=%d", action.Selection.Id),
						fmt.Sprintf("paguridaesaddr=#%d,#%d", action.Selection.Range.Index,
							action.Selection.Range.Index+action.Selection.Range.Length))
					if pipeSelectionToStdin {
						d := c.Server.CurrentChange(action.Selection.Id).Change.Delta.Slice(
							int(action.Selection.Range.Index),
							int(action.Selection.Range.Index+action.Selection.Range.Length))
						cmd.Stdin = strings.NewReader(DeltaToString(*d))
					}
					w := c.newErrorBuffer(action)
					cmd.Stderr = w
					var stdoutBuffer bytes.Buffer
					if pipeStdoutToSelection {
						cmd.Stdout = &stdoutBuffer
					} else {
						cmd.Stdout = w
					}
					err = cmd.Start()
					if err != nil {
						return err
					}
					if pipeStdoutToSelection {
						// Wait for command to finish first
						err = cmd.Wait()
						if err != nil {
							return err
						}
						cancelCmd()
						// Grab stdout data and modify selection
						c.Server.Submit(SystemClientId, ot.MultiFileChange{
							Id: action.Selection.Id,
							Change: ot.Change{
								Version: c.Server.CurrentChange(action.Selection.Id).Change.Version,
								Delta: delta.New(nil).
									Retain(int(action.Selection.Range.Index), nil).
									Delete(int(action.Selection.Range.Length)).
									Insert(string(stdoutBuffer.String()), nil),
							},
						})
					}
				}
			}
			return nil
		}
	} else {
		return errors.New(fmt.Sprint("Unknown action type:", action.Type))
	}
}

type errorsBufferWriter struct {
	contentFileId uint32
	path          string
	c             *Connection
}

func (w *errorsBufferWriter) Write(p []byte) (n int, err error) {
	if w.contentFileId == 0 {
		// Initialize file ID
		w.contentFileId, err = w.c.FindOrCreateDummyFile(filepath.Join(w.path, "+Errors"))
		if err != nil {
			return
		}
	}
	w.c.Server.Append(SystemClientId, w.contentFileId, string(p))
	return len(p), nil
}
