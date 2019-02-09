package main

import (
	"github.com/fmpwizard/go-quilljs-delta/delta"
)

type EditorChange struct {
	Id      uint32       `json:"id"`
	Label   *delta.Delta `json:"label,omitempty"`
	Content *delta.Delta `json:"content,omitempty"`
}

type Response struct {
	// All the active row IDs, note that backend doesn't have the concept
	// of columns, nor does it know which column a row is in.
	Ids []uint32 `json:"ids"`
	// Changes to be synced to the frontend
	Changes []EditorChange `json:"changes,omitempty"`
}

type Action struct {
	Id     uint32
	Type   string
	Index  uint32
	Length uint32
}

type Request struct {
	// Changes to be synced to the backend, notice the changes here are
	// expected to happen before the included action(if exists)
	Changes []EditorChange `json:"changes,omitempty"`
	action  *Action        `json:"changes,omitempty"`
}
