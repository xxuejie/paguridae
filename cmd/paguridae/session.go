package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/google/uuid"
	"xuejie.space/c/go-quill-editor"
	"xuejie.space/c/paguridae/pkg/ot"
)

const (
	DefaultLabel          = " | New Del Put"
	MetaFileId            = 0
	CommandTimeoutSeconds = 10
)

var (
	AbsolutePathRe = regexp.MustCompile(`^^[,\d\(\)]*/`)
	PathRe         = regexp.MustCompile(`^(?:\((\d+),(\d+)(?:,(\d+))?\))?(.*)$`)
)

type fullPathInfo struct {
	path     string
	location string

	start  *int64
	length *int64

	fileLength *int64
}

func (i fullPathInfo) same(other fullPathInfo) bool {
	if i.path != other.path || i.location != other.location ||
		i.partialLoad() != other.partialLoad() {
		return false
	}
	if i.partialLoad() && (*i.start != *other.start || *i.length != *other.length) {
		return false
	}
	return true
}

func (i fullPathInfo) partialLoad() bool {
	return i.start != nil && i.length != nil
}

func (i fullPathInfo) serializePath() string {
	var prefix string
	if i.partialLoad() {
		if i.fileLength != nil {
			prefix = fmt.Sprintf("(%d,%d,%d)", *i.start, *i.length, *i.fileLength)
		} else {
			prefix = fmt.Sprintf("(%d,%d)", *i.start, *i.length)
		}
	}
	return fmt.Sprintf("%s%s", prefix, i.path)
}

func (i fullPathInfo) serialize() string {
	var suffix string
	if len(i.location) > 0 {
		suffix = fmt.Sprintf(":%s", suffix)
	}
	return fmt.Sprintf("%s%s", i.serializePath(), suffix)
}

func extractFullPath(d delta.Delta) string {
	return strings.SplitN(DeltaToString(d, false), " ", 2)[0]
}

func parseFullPath(fullPath string) (info fullPathInfo) {
	// Extract potential content location argument
	parts := strings.SplitN(fullPath, ":", 2)
	matches := PathRe.FindStringSubmatch(parts[0])
	if len(matches) != 5 {
		log.Printf("Error extracting path from %s", fullPath)
		return
	}
	info.path = filepath.Clean(matches[4])
	if len(parts) == 2 {
		info.location = parts[1]
	}
	startInt, startErr := strconv.ParseInt(matches[1], 10, 64)
	lengthInt, lengthErr := strconv.ParseInt(matches[2], 10, 64)
	if startErr == nil && lengthErr == nil {
		info.start = &startInt
		info.length = &lengthInt
	}
	fileLengthInt, fileLengthErr := strconv.ParseInt(matches[3], 10, 64)
	if fileLengthErr == nil {
		info.fileLength = &fileLengthInt
	}
	return
}

func extractPath(d delta.Delta) fullPathInfo {
	return parseFullPath(extractFullPath(d))
}

type Session struct {
	sessionId     uuid.UUID
	clientId      uuid.UUID
	NextId        uint32
	Server        *ot.Server
	VerifyContent bool

	clientFlushChans map[uuid.UUID](chan bool)
	listenPath       string
	listener         net.Listener
	listenerSignal   chan bool
	mux              sync.Mutex
}

func NewSession(verifyContent bool) (*Session, error) {
	sessionId := uuid.New()
	listenPath := fmt.Sprintf("/tmp/paguridae/%s", sessionId)
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

	server := ot.NewServer()
	server.ErrorProcessor = func(err error) {
		log.Printf("OT server encountered errors: %v", err)
	}
	go func() {
		server.Start()
	}()
	userEvents := server.Connect(nil)
	initialEvent := <-userEvents
	id := *initialEvent.ConnectedClientId

	session := &Session{
		sessionId:        sessionId,
		clientId:         id,
		NextId:           MetaFileId + 1,
		Server:           server,
		VerifyContent:    verifyContent,
		clientFlushChans: make(map[uuid.UUID](chan bool)),
		listenPath:       listenPath,
		listener:         listener,
		listenerSignal:   make(chan bool),
	}

	metaFileChan := make(chan bool)
	go func() {
		for event := range userEvents {
			if len(event.CreatedFileIds) > 0 ||
				len(event.ClosedFileIds) > 0 {
				metaFileChan <- true
			}
		}
	}()
	go func() {
		for range metaFileChan {
			session.refreshMetafile()
		}
	}()
	// Creating meta file, meta file ID must be 0
	ids := server.CreateFiles(*delta.New(nil))
	if ids[0] != MetaFileId {
		server.Stop()
		return nil, fmt.Errorf("Unexpected meta file ID: %d", ids[0])
	}
	// A new session has 2 files: an empty one, and one showing
	// contents from current directory
	currentPath, err := os.Getwd()
	if err != nil {
		server.Stop()
		return nil, err
	}
	_, err = session.CreateDummyFile()
	if err != nil {
		server.Stop()
		return nil, err
	}
	err = session.CreateDirectoryListingFile(currentPath)
	if err != nil {
		server.Stop()
		return nil, err
	}
	err = Start9PFileSystem(session)
	if err != nil {
		server.Stop()
		return nil, err
	}
	return session, nil
}

func (s *Session) Id() uuid.UUID {
	return s.sessionId
}

func (s *Session) Connect(clientId *uuid.UUID) (uuid.UUID, <-chan ot.Event, <-chan bool) {
	userEvents := s.Server.Connect(clientId)
	initialEvent := <-userEvents
	id := *initialEvent.ConnectedClientId
	flushChan := make(chan bool)

	s.mux.Lock()
	s.clientFlushChans[id] = flushChan
	s.mux.Unlock()

	return id, userEvents, flushChan
}

func (s *Session) Disconnect(clientId uuid.UUID) {
	s.Server.Disconnect(clientId)

	s.mux.Lock()
	defer s.mux.Unlock()
	delete(s.clientFlushChans, clientId)
}

func (s *Session) Connections() int {
	s.mux.Lock()
	defer s.mux.Unlock()

	return len(s.clientFlushChans)
}

func (s *Session) Flush() {
	flushChans := make([]chan bool, 0, len(s.clientFlushChans))

	s.mux.Lock()
	for _, flushChan := range s.clientFlushChans {
		flushChans = append(flushChans, flushChan)
	}
	s.mux.Unlock()

	for _, flushChan := range flushChans {
		flushChan <- true
	}
}

func (s *Session) Stop() {
	close(s.listenerSignal)
	s.listener.Close()
	os.Remove(s.listenPath)
	s.Server.Stop()
}

func idToMeta(id uint32) delta.Delta {
	return *delta.New(nil).Insert(fmt.Sprintf("%d 0 0\n", id), nil)
}

func (s *Session) refreshMetafile() {
	err := s.Server.UpdateAll(func(changes []ot.ServerUpdate) ([]ot.ClientChange, error) {
		var oldMeta *ot.ServerUpdate
		for _, change := range changes {
			if change.Id == MetaFileId {
				oldMeta = &change
				break
			}
		}
		if oldMeta == nil {
			return nil, fmt.Errorf("Metafile does not exist, something is seriously wrong!")
		}
		oldInfos := make(map[uint32]string)
		for _, line := range strings.Split(DeltaToString(oldMeta.Delta, true), "\n") {
			if len(line) == 0 {
				continue
			}
			pieces := strings.SplitN(line, " ", 2)
			if len(pieces) != 2 {
				return nil, fmt.Errorf("Invalid split for line: %s", line)
			}
			i, err := strconv.ParseUint(pieces[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("Unexpected parse error: %v", err)
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
		d := Diff(oldMeta.Delta, *newMetaContent)
		return []ot.ClientChange{
			{
				Id:    MetaFileId,
				Delta: *d,
				Base:  oldMeta.Version,
			},
		}, nil
	})
	if err != nil {
		log.Printf("Refreshing metafile error: %v", err)
	} else {
		s.Server.Broadcast()
	}
}

func (s *Session) closeFile(fileId uint32) {
	s.Server.CloseFiles(fileId)
}

func (s *Session) createFile(label string, content *string) (uint32, error) {
	contentDelta := delta.New(nil)
	if content != nil {
		contentDelta = contentDelta.Insert(*content, nil)
	}
	ids := s.Server.CreateFiles(*delta.New(nil).Insert(label, nil), *contentDelta)
	labelId := ids[0]
	contentId := ids[1]
	if labelId%2 != 1 || contentId != labelId+1 {
		s.Server.CloseFiles(labelId, contentId)
		return 0, fmt.Errorf("Unexpected allocated file IDs: %d %d", labelId, contentId)
	}
	return contentId, nil
}

func (s *Session) CreateDummyFile() (uint32, error) {
	return s.createFile(DefaultLabel, nil)
}

func (s *Session) FindOrCreateDummyFile(path string) (uint32, error) {
	for _, change := range s.Server.AllContents() {
		if change.Id%2 != 0 {
			if extractPath(change.Delta).path == path {
				// Return content Id based on current label Id
				return change.Id + 1, nil
			}
		}
	}
	return s.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), nil)
}

func (s *Session) CreateDirectoryListingFile(path string) error {
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
	_, err = s.createFile(fmt.Sprintf("%s%s", path, DefaultLabel), &content)
	return err
}

func samSearch(file editor.File, location string) (*Range, error) {
	if len(location) == 0 {
		return nil, nil
	}
	compiledCmd, err := editor.Compile(fmt.Sprintf("%s=", location))
	if err != nil {
		return nil, err
	}
	err = compiledCmd.Run(editor.Context{
		File: file,
	})
	if err != nil {
		return nil, err
	}
	q0, q1 := file.Dot()
	return &Range{
		Index:  uint32(q0),
		Length: uint32(q1 - q0),
	}, nil
}

func (s *Session) readAndCreateFile(path string, sizeCalculator func(info os.FileInfo) (*int64, *int64)) (uint32, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	stat, err := file.Stat()
	if err != nil {
		return 0, "", err
	}
	start, length := sizeCalculator(stat)
	var content []byte
	var label string
	if length != nil {
		content = make([]byte, *length)
		label = fmt.Sprintf("(%d,%d,%d)%s%s", *start, *length, stat.Size(), path, DefaultLabel)
	} else {
		content = make([]byte, stat.Size())
		label = fmt.Sprintf("%s%s", path, DefaultLabel)
	}
	if start != nil {
		_, err = file.Seek(*start, os.SEEK_SET)
		if err != nil {
			return 0, "", err
		}
	}
	_, err = file.Read(content)
	if err != nil {
		return 0, "", err
	}
	contentString := string(content)
	contentId, err := s.createFile(label, &contentString)
	if err != nil {
		return 0, "", err
	}
	return contentId, contentString, nil
}

func (s *Session) FindOrOpenFile(pathInfo fullPathInfo) (*Selection, bool, error) {
	allContents := s.Server.AllContents()
	var labelId uint32
	for _, change := range allContents {
		if change.Id%2 != 0 {
			if pathInfo.same(extractPath(change.Delta)) {
				labelId = change.Id
				break
			}
		}
	}
	if labelId != 0 {
		contentId := labelId + 1
		for _, change := range allContents {
			if change.Id == contentId {
				r, err := samSearch(editor.NewDeltaFile(change.Delta), pathInfo.location)
				// Sam search error is ignored here.
				if err != nil {
					log.Printf("Sam search error %v", err)
				}
				if r == nil {
					r = &Range{
						Index:  0,
						Length: 0,
					}
				}
				return &Selection{
					Id:    contentId,
					Range: *r,
				}, false, nil
			}
		}
		return nil, false, fmt.Errorf("Label file %d is found but content file %d is missing!", labelId, contentId)
	}
	contentId, contentString, err := s.readAndCreateFile(pathInfo.path, func(info os.FileInfo) (*int64, *int64) {
		if pathInfo.partialLoad() {
			start := *pathInfo.start
			if start < 0 {
				start = 0
			}
			length := *pathInfo.length
			remainingLength := info.Size() - *pathInfo.start
			if length > remainingLength {
				length = remainingLength
			}
			return &start, &length
		}
		if info.Size() <= int64(*pageSize) {
			return nil, nil
		}
		start := int64(0)
		length := int64(*pageSize)
		return &start, &length
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	// TODO: should we support sam search in arbitrary file location?
	r, err := samSearch(
		editor.NewDeltaFile(*delta.New(nil).Insert(contentString, nil)),
		pathInfo.location)
	// Sam search error is ignored here.
	if err != nil {
		log.Printf("Sam search error %v", err)
	}
	if r == nil {
		r = &Range{
			Index:  0,
			Length: 0,
		}
	}
	return &Selection{
		Id:    contentId,
		Range: *r,
	}, true, nil
}

func (s *Session) deleteFile(action Action) {
	s.closeFile(action.LabelId())
	s.closeFile(action.ContentId())
}

func (s *Session) editFile(action Action) {
	var errorBuffer bytes.Buffer
	completeChan := make(chan bool, 1)
	err := s.Server.Update(action.ContentId(), func(d delta.Delta) (delta.Delta, error) {
		f := editor.NewDeltaFile(d)
		f.Select(int64(action.Selection.Range.Index),
			int64(action.Selection.Range.Index+action.Selection.Range.Length))
		cmdStr := action.Command[4:]
		cmd, err := editor.Compile(cmdStr)
		if err != nil {
			return *delta.New(nil), err
		}
		err = cmd.Run(editor.Context{
			File:    f,
			Printer: &errorBuffer,
		})
		completeChan <- (err == nil)
		if err != nil {
			return *delta.New(nil), err
		}
		return f.Changes(), nil
	})
	if err != nil {
		log.Printf("Editing file encountered error: %v", err)
		return
	}
	if <-completeChan {
		if errorBuffer.Len() > 0 {
			labelId := action.LabelId()
			s.newErrorBuffer(&labelId).Write(errorBuffer.Bytes())
		}
		// This will result in false positives, but let's stick with the simple path now
		s.markDirty(action.ContentId())
		s.Server.Broadcast()
	}
}

func (s *Session) newErrorBuffer(labelId *uint32) *errorsBufferWriter {
	var path string
	if labelId != nil {
		path = filepath.Dir(extractPath(s.Server.Content(*labelId).Delta).path)
	}
	return &errorsBufferWriter{
		path: path,
		s:    s,
	}
}

func (s *Session) runSamCommand(fileId uint32, cmd string) error {
	compiledCmd, err := editor.Compile(cmd)
	if err != nil {
		return err
	}
	return s.Server.Update(fileId, func(d delta.Delta) (delta.Delta, error) {
		f := editor.NewDeltaFile(d)
		err := compiledCmd.Run(editor.Context{
			File: f,
		})
		return f.Changes(), err
	})
}

func (s *Session) markDirty(contentId uint32) error {
	return s.runSamCommand(contentId-1, `1s/\|\*?/|*/`)
}

func (s *Session) markClean(contentId uint32) error {
	return s.runSamCommand(contentId-1, `1s/\|\*/|/`)
}

func (s *Session) ApplyChanges(clientId uuid.UUID, changes []ot.ClientChange) error {
	for _, change := range changes {
		// Ignore client changes to meta file.
		if change.Id > 0 {
			s.Server.Submit(&clientId, change)
			err := s.markDirty(change.Id)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Session) Execute(clientId uuid.UUID, action Action) (*Selection, bool, error) {
	labelContent := s.Server.Content(action.LabelId())
	if labelContent == nil {
		return nil, false, fmt.Errorf("Cannot find label file: %d, something must be wrong", action.LabelId())
	}
	labelPath := extractFullPath(labelContent.Delta)

	if action.Type == "search" {
		var fullPath string
		if AbsolutePathRe.MatchString(action.Command) {
			fullPath = action.Command
		} else {
			fullPath = labelPath
			if !strings.HasSuffix(fullPath, "/") {
				fullPath += "/../"
			}
			fullPath += action.Command
		}
		pathInfo := parseFullPath(fullPath)
		stat, err := os.Stat(pathInfo.path)
		if err != nil {
			if os.IsNotExist(err) {
				update := s.Server.Content(action.ContentId())
				if update == nil {
					return nil, false, nil
				}
				content := DeltaToRunes(update.Delta, false)
				target := []rune(action.Command)
				length := uint32(len(target))
				start := action.Index + length
				var found bool
				for ; start+length <= uint32(len(content)); start++ {
					found = true
					for j := range target {
						if content[int(start)+j] != target[j] {
							found = false
							break
						}
					}
					if found {
						break
					}
				}
				if found {
					return &Selection{
						Id: action.ContentId(),
						Range: Range{
							Index:  start,
							Length: length,
						},
					}, false, nil
				} else {
					return nil, false, nil
				}
			} else {
				return nil, false, err
			}
		}
		if stat.IsDir() {
			err = s.CreateDirectoryListingFile(pathInfo.path)
		} else {
			return s.FindOrOpenFile(pathInfo)
		}
		if err != nil {
			return nil, false, err
		}
		return nil, false, err
	} else if action.Type == "execute" {
		return s.execute(parseFullPath(labelPath), action)
	} else {
		return nil, false, errors.New(fmt.Sprint("Unknown action type:", action.Type))
	}
}

func (s *Session) execute(pathInfo fullPathInfo, action Action) (*Selection, bool, error) {
	switch action.Command {
	case "New":
		_, err := s.CreateDummyFile()
		return nil, false, err
	case "Del":
		s.deleteFile(action)
		return nil, false, nil
	case "Undo":
		// Undo error is ignored
		s.Server.Undo(action.ContentId())
		return nil, false, nil
	case "Redo":
		// Redo error is ignored
		s.Server.Redo(action.ContentId())
		return nil, false, nil
	case "Next":
		if !pathInfo.partialLoad() {
			return nil, false, nil
		}
		newStart := *pathInfo.start + int64(*scrollSize)
		newLength := int64(*pageSize)
		pathInfo.start = &newStart
		pathInfo.length = &newLength
		return s.FindOrOpenFile(pathInfo)
	case "Prev":
		if !pathInfo.partialLoad() {
			return nil, false, nil
		}
		newStart := *pathInfo.start - int64(*scrollSize)
		if newStart < 0 {
			newStart = 0
		}
		newLength := int64(*pageSize)
		pathInfo.start = &newStart
		pathInfo.length = &newLength
		return s.FindOrOpenFile(pathInfo)
	case "Put":
		if action.Id == MetaFileId || len(pathInfo.path) == 0 {
			return nil, false, nil
		}
		fileContent := s.Server.Content(action.ContentId())
		if fileContent == nil {
			return nil, false, fmt.Errorf("Cannot find file %d to save!", action.ContentId())
		}
		var data []byte
		if fileContent != nil {
			// Put command here ignores all embeds and just save texts to a file, later
			// we can add a different command that do save embeds in the buffer
			data = []byte(DeltaToString(fileContent.Delta, false))
		}
		savingFile, err := ioutil.TempFile(filepath.Dir(pathInfo.path), "saving")
		if err != nil {
			return nil, false, err
		}
		savingFilename := savingFile.Name()
		sourceFile, err := os.Open(pathInfo.path)
		if err != nil {
			return nil, false, err
		}
		sourceFileStat, err := sourceFile.Stat()
		if err != nil {
			return nil, false, err
		}
		if pathInfo.partialLoad() && *pathInfo.start > 0 {
			_, err = io.CopyN(savingFile, sourceFile, *pathInfo.start)
			if err != nil {
				savingFile.Close()
				os.Remove(savingFilename)
				return nil, false, err
			}
		}
		_, err = savingFile.Write(data)
		if err != nil {
			savingFile.Close()
			os.Remove(savingFilename)
			return nil, false, err
		}
		if pathInfo.partialLoad() && *pathInfo.start+*pathInfo.length < sourceFileStat.Size() {
			remainingStart := *pathInfo.start + *pathInfo.length
			_, err = sourceFile.Seek(remainingStart, os.SEEK_SET)
			if err != nil {
				savingFile.Close()
				os.Remove(savingFilename)
				return nil, false, err
			}
			_, err = io.CopyN(savingFile, sourceFile, sourceFileStat.Size()-remainingStart)
			if err != nil {
				savingFile.Close()
				os.Remove(savingFilename)
				return nil, false, err
			}
		}
		err = savingFile.Close()
		if err != nil {
			return nil, false, err
		}
		err = os.Rename(savingFilename, pathInfo.path)
		if err != nil {
			return nil, false, err
		}
		return nil, false, s.markClean(action.ContentId())
	default:
		if strings.HasPrefix(action.Command, "Edit") {
			s.editFile(action)
			return nil, false, nil
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
				// acmeaddr is different from paguridae addr. acmeaddr describes the command
				// argument sent via mouse chording, while paguridaesaddr describes the addr
				// for selected texts passed in via pipes. Later if we decide to add mouse
				// chording, we can then include acmeaddr here.
				cmd.Env = append(os.Environ(),
					fmt.Sprintf("winid=%d", action.Id),
					fmt.Sprintf("%%=%s", pathInfo.path),
					fmt.Sprintf("samfile=%s", pathInfo.path),
					fmt.Sprintf("paguridae_session=%s", s.Id()),
					fmt.Sprintf("paguridae_selection_id=%d", action.Selection.Id),
					fmt.Sprintf("paguridae_selection_addr=#%d,#%d", action.Selection.Range.Index,
						action.Selection.Range.Index+action.Selection.Range.Length))
				if pipeSelectionToStdin {
					d := s.Server.Content(action.Selection.Id).Delta.Slice(
						int(action.Selection.Range.Index),
						int(action.Selection.Range.Index+action.Selection.Range.Length))
					cmd.Stdin = strings.NewReader(DeltaToString(*d, false))
				}
				labelId := action.LabelId()
				w := s.newErrorBuffer(&labelId)
				cmd.Stderr = w
				var stdoutBuffer bytes.Buffer
				if pipeStdoutToSelection {
					cmd.Stdout = &stdoutBuffer
				} else {
					cmd.Stdout = w
				}
				err = cmd.Start()
				if err != nil {
					return nil, false, err
				}
				if pipeStdoutToSelection {
					// Wait for command to finish first
					err = cmd.Wait()
					if err != nil {
						return nil, false, err
					}
					cancelCmd()
					// Grab stdout data and modify selection
					oldContent := s.Server.Content(action.Selection.Id)
					s.Server.Submit(nil, ot.ClientChange{
						Id:   action.Selection.Id,
						Base: oldContent.Version,
						Delta: *delta.New(nil).
							Retain(int(action.Selection.Range.Index), nil).
							Delete(int(action.Selection.Range.Length)).
							Insert(string(stdoutBuffer.String()), nil),
					})
				}
			}
		}
		return nil, false, nil
	}
}

type errorsBufferWriter struct {
	contentFileId uint32
	path          string
	s             *Session
}

func (w *errorsBufferWriter) Write(p []byte) (n int, err error) {
	if w.contentFileId == 0 {
		// Initialize file ID
		w.contentFileId, err = w.s.FindOrCreateDummyFile(filepath.Join(w.path, "+Errors"))
		if err != nil {
			return
		}
	}
	w.s.Server.Append(w.contentFileId, []rune(string(p)))
	return len(p), nil
}
