package postgres

import (
	"encoding/json"
	"strconv"

	easyjson "github.com/mailru/easyjson"
	_ "github.com/mailru/easyjson/gen"
	jwriter "github.com/mailru/easyjson/jwriter"
)

//easyjson:json
type bulkHeaderPayload struct {
	Index   string `json:"_index"`
	ID      string `json:"_id,omitempty"`
	Routing string `json:"routing,omitempty"`
}

//easyjson:json
type bulkHeader struct {
	// maybe it's even possible to make those fields private
	Insert *bulkHeaderPayload `json:"insert,omitempty"`
	Update *bulkHeaderPayload `json:"update,omitempty"`
	Delete *bulkHeaderPayload `json:"delete,omitempty"`
	Index  *bulkHeaderPayload `json:"index,omitempty"`
}

// newHeader returns pointers for header and subfield payload inside a header
func newHeader(action ESAction) (h *bulkHeader, p *bulkHeaderPayload) {
	h = &bulkHeader{}
	p = &bulkHeaderPayload{}

	switch action {
	case ESInsert:
		h.Insert = p
	case ESUpdate:
		h.Update = p
	case ESDelete:
		h.Delete = p
	case ESIndex:
		h.Index = p
	}

	return h, p
}

type stringKV struct {
	key   string
	value string
}

func (kv stringKV) jsonKey() string {
	return kv.key
}

func (kv stringKV) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(kv.value)), nil // TODO proper marshaling
}

// not sure if useful.
// type renameKV struct {
// jsonKV
// name string
// }
// func (rename renameKV) jsonKey() string {
// return rename.name
// }

// real and ephemeral columns. Document KVs
type jsonKV interface {
	jsonKey() string
}

// document represents json document, that would be sent to search.
// attempts to split table config from data.
type document struct {
	fields []jsonKV // table fields
}

// MarshalJSON supports json.Marshaler interface
func (v document) MarshalJSON() ([]byte, error) {
	w := jwriter.Writer{}
	v.MarshalEasyJSON(&w)
	return w.Buffer.BuildBytes(), w.Error
}

// MarshalEasyJSON supports easyjson.Marshaler interface
// Not generated; however easyjson genearated code was a reference
func (v document) MarshalEasyJSON(out *jwriter.Writer) {
	out.RawByte('{')
	for i, col := range v.fields {
		if i > 0 {
			out.RawByte(',')
		}
		out.String(col.jsonKey())
		out.RawByte(':')

		if m, ok := col.(easyjson.Marshaler); ok {
			m.MarshalEasyJSON(out)
		} else if m, ok := col.(json.Marshaler); ok {
			out.Raw(m.MarshalJSON())
		} else {
			out.Raw(json.Marshal(col))
		}
	}
	out.RawByte('}')
}

//easyjson:json
type documentJoin struct {
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"` // omitempty is important
}
