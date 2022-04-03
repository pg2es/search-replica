package postgres

import (
	"bytes"
	"encoding/json"

	jwriter "github.com/mailru/easyjson/jwriter"
	"go.uber.org/zap"
)

// Inline defines table like abstraction for inligning into ES doc
type Inline struct {
	name      string
	fieldName string
	parent    *Table
	source    *Table

	// pointers to columns of source table
	// with defined positions
	// temporarily: renaming fields is not possible in this context
	columns map[string]*Column // By name

	pkCol      *Column
	parentCol  *Column
	routingCol *Column
	upsertOnly bool // without old PKs / Parent / _routing in WAL, proper update & delete is impossible

	scriptAddID string
	scriptDelID string

	logger *zap.Logger
}

// returns inline PK column, which is PK of source table by default
func (i *Inline) pk() *Column {
	if i.pkCol == nil {
		i.pkCol = i.source.pkCol
	}
	return i.pkCol
}

func (i *Inline) init() {
	if i.parentCol == nil {
		i.logger.Fatal("parent column is not configured")
	}

	if i.pkCol == nil && i.source.pkCol != nil {
		i.pkCol = i.source.pkCol
		i.logger.Info("using implicit PK column")
	}
	if i.pkCol == nil {
		i.logger.Fatal("PK column is not configured")
	}

	if !i.pkCol.oldInWAL || !i.parentCol.oldInWAL {
		i.upsertOnly = true
	}
	if i.routingCol != nil && !i.routingCol.oldInWAL {
		i.upsertOnly = true
	}
}

// keysChanged tells whether document needs to be recreated
func (i *Inline) keysChanged(oldRow, newRow [][]byte) bool {
	if !bytes.Equal(newRow[i.parentCol.pos], oldRow[i.parentCol.pos]) {
		return true
	}
	if i.routingCol != nil && !bytes.Equal(newRow[i.routingCol.pos], oldRow[i.routingCol.pos]) {
		return true
	}
	return false
}

func (i *Inline) elasticBulkHeader(action ESAction) ([]byte, error) {
	header, payload := newHeader(ESUpdate) // Always Update

	payload.Index = i.parent.indexName
	payload.ID = i.parentCol.string()
	if !i.parent.pkNoPrefix {
		payload.ID = i.parent.name + "_" + payload.ID
	}
	if i.routingCol != nil {
		payload.Routing = i.routingCol.string()
	}

	return json.Marshal(header)
}

func (inline *Inline) jsonEncodeRow(buf *jwriter.Writer) {
	doc := document{}
	for _, col := range inline.columns { // add real columns
		doc.fields = append(doc.fields, col)
	}
	doc.MarshalEasyJSON(buf)
}

func (inline *Inline) jsonAddScript() ([]byte, error) {
	out := jwriter.Writer{}
	out.RawString(`{"scripted_upsert":true,"script":{"id":`)
	out.String(inline.scriptAddID)
	out.RawString(`,"params":{"obj":`)

	// New object is passed as params
	// XXX: maybe it makes sense to wrap values into additional params struct, so we can pass arguments there.
	inline.jsonEncodeRow(&out)

	out.RawString(`,"pk":`)
	out.String(inline.pk().name)
	out.RawString(`,"inline":`)
	out.String(inline.fieldName)
	out.RawByte('}')

	pCol := inline.parentCol
	out.RawString(`},`)

	// default values for empty document with inlined field
	// TODO: reuse from parent table or remove completely
	out.RawString(`"upsert":{"docType":`)

	out.String(inline.parent.name)
	out.RawByte(',')
	out.String(inline.parent.pkCol.name)
	out.RawByte(':')
	out.Raw(pCol.MarshalJSON())

	out.RawString(`}}`)

	return out.Buffer.BuildBytes(), out.Error
}

func (inline *Inline) jsonDelScript() ([]byte, error) {
	out := jwriter.Writer{}
	out.RawString(`{"script":{"id":`)
	out.String(inline.scriptDelID)
	out.RawString(`,"params":{"obj":`)

	inline.jsonEncodeRow(&out)

	out.RawString(`,"pk":`)
	out.String(inline.pk().name)
	out.RawString(`,"inline":`)
	out.String(inline.fieldName)
	out.RawString(`}`)

	out.RawString(`},"scripted_upsert":false}`)

	return out.Buffer.BuildBytes(), out.Error
}
