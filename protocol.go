package main

import (
	"github.com/fmpwizard/go-quilljs-delta/delta"
)

type Change struct {
	Id      int         `json:"id"`
	Delta   delta.Delta `json:"delta"`
	Version int         `json:"version"`
}

func (c Change) FileId() int {
	return c.Id - 1 + c.Id%2
}

type Action struct {
	Id        int    `json:"id"`
	Action    string `json:"action"`
	Index     int    `json:"index"`
	Selection string `json:"selection"`
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
