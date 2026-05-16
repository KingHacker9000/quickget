package events

import (
	"encoding/json"
	"io"
	"sync"
)

type Emitter struct {
	w  io.Writer
	mu sync.Mutex
}

func NewEmitter(w io.Writer) *Emitter {
	return &Emitter{w: w}
}

func (e *Emitter) Emit(event any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = e.w.Write(data)
	return err
}
