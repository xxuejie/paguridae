package main

import (
	"github.com/xxuejie/go-delta-ot/ot"
)

type Range struct {
	Index  uint32 `json:"index"`
	Length uint32 `json:"length"`
}

type Selection struct {
	Id    uint32 `json:"id"`
	Range Range  `json:"range"`
}

type Action struct {
	Id        uint32    `json:"id"`
	Type      string    `json:"type"`
	Index     uint32    `json:"index"`
	Command   string    `json:"command"`
	Selection Selection `json:"selection"`
}

func (a Action) LabelId() uint32 {
	return a.Id - 1 + a.Id%2
}

func (a Action) ContentId() uint32 {
	return a.LabelId() + 1
}

type Request struct {
	Changes []ot.MultiFileChange `json:"changes"`
	Action  Action               `json:"action"`
}

type Update struct {
	Changes []ot.MultiFileChange `json:"changes,omitempty"`
	Hashes  map[uint32]string    `json:"hashes,omitempty"`
}
