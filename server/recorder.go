package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-fed/activity/streams"
	"github.com/go-fed/activity/streams/vocab"
)

type Entry struct {
	T   time.Time
	Msg string
	M   []byte
}

func (e Entry) MString() string {
	return string(e.M)
}

type Recorder struct {
	entries []Entry
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Entries() []Entry {
	// TODO: Mutex
	return r.entries
}

func (r *Recorder) Add(msg string, i ...interface{}) {
	now := time.Now().UTC()
	rec := make([][]byte, len(i))
	for idx, val := range i {
		switch t := val.(type) {
		case vocab.Type:
			m, err := streams.Serialize(t)
			if err != nil {
				rec[idx] = []byte("could not serialize vocab.Type for logging")
				continue
			}
			b, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				rec[idx] = []byte(err.Error())
				continue
			}
			rec[idx] = b
		case fmt.Stringer:
			rec[idx] = []byte(t.String())
		case error:
			rec[idx] = []byte(t.Error())
		default:
			rec[idx] = []byte(fmt.Sprintf("%v", t))
		}
	}
	for idx, b := range rec {
		rec[idx] = append([]byte(fmt.Sprintf("[%d] ", idx)), b...)
	}
	r.entries = append(r.entries,
		Entry{
			T:   now,
			Msg: msg,
			M:   bytes.Join(rec, []byte("\n")),
		})
}
