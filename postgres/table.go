package postgres

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/jackc/pglogrepl"
	jwriter "github.com/mailru/easyjson/jwriter"
	"go.uber.org/zap"
)

type Table struct {
	schema  *Schema            // RO
	columns map[string]*Column // By name

	inlines     []*Inline // inline name -> table which uses it. owns
	isInlinedIn []*Inline // list of tables, this table `is inlined in`; We need to update in ES documents of those tables.

	name  string
	relID uint32 // used in logical_replication protocol

	docType string // name of document type within index

	index      bool
	indexAll   bool // index all columns by default
	noid       bool // skip _id field generation
	upsertOnly bool // without old PKs / _routing in WAL, proper update & delete is impossible

	tagParsed bool

	pkCol      *Column // used in scripting and `_id`
	pkNoPrefix bool    // use raw field instead of {table}_{pk}
	routingCol *Column // value for `_routing`

	join tableJoin

	indexName string // quoted and escaped value
	logger    *zap.Logger
}

// Table returns column's owner table
func (t *Table) Schema() *Schema {
	return t.schema
}

// TODO: take RelID set cache out of this tree config
func (t *Table) SetRelationID(id uint32) {
	t.relID = id
}

type ESAction string

const (
	ESInsert ESAction = "insert"
	ESUpdate ESAction = "update"
	ESDelete ESAction = "delete"
	ESIndex  ESAction = "index" // Upsert
)

// keysChanged tells whether document needs to be recreated
func (t *Table) keysChanged(oldRow, newRow [][]byte) bool {
	if !bytes.Equal(newRow[t.pkCol.pos], oldRow[t.pkCol.pos]) {
		return true
	}
	if t.routingCol != nil && !bytes.Equal(newRow[t.routingCol.pos], oldRow[t.routingCol.pos]) {
		return true
	}
	return false
}

func (t *Table) elasticBulkHeader(action ESAction, row [][]byte) ([]byte, error) {
	header, payload := newHeader(action)

	payload.Index = t.indexName
	payload.ID = t.pkCol.stringFromRow(row)
	if !t.pkNoPrefix { // add document type prefix to ID, to avoid collisions
		payload.ID = t.name + "_" + payload.ID
	}
	if t.routingCol != nil {
		payload.Routing = t.routingCol.stringFromRow(row)
	}

	return json.Marshal(header)
}

func rowFromPGTuple(tuple *pglogrepl.TupleData) [][]byte {
	row := make([][]byte, len(tuple.Columns))
	for i, tupleCol := range tuple.Columns {
		row[i] = tupleCol.Data
	}
	return row
}

func (t *Table) EncodeRowJSON(row [][]byte) ([]byte, error) {
	out := jwriter.Writer{}
	t.jsonEncodeRow(&out, row)
	return out.Buffer.BuildBytes(), out.Error
}

// EncodeUpdateRowJSON generates json.
// during updates, new document should be wrapper into a {"doc": ... } object
func (t *Table) EncodeUpdateRowJSON(row [][]byte) ([]byte, error) {
	out := jwriter.Writer{}
	out.RawString(`{"doc":`)

	t.jsonEncodeRow(&out, row)

	out.RawByte('}')
	return out.Buffer.BuildBytes(), out.Error
}

func (t *Table) jsonEncodeRow(buf *jwriter.Writer, row [][]byte) {
	doc := document{row: row}
	for _, col := range t.columns { // add real columns
		if col.index {
			doc.fields = append(doc.fields, col)
		}
	}
	if t.join.enabled {
		doc.fields = append(doc.fields, &t.join)
	}
	doc.fields = append(doc.fields, stringKV{
		key: "docType", value: t.docType,
	})

	doc.MarshalEasyJSON(buf)
}

// init: consistency checks and pre-encode caching
func (t *Table) init() {
	for _, inl := range t.isInlinedIn {
		inl.init()
	}

	if !t.index { // Skip checks & setup for ignored tables
		return
	}

	if t.pkCol == nil {
		t.logger.Debug("PK is not configured. Fallback to implicit PK")
		for _, col := range t.columns {
			if col.sqlPK {
				t.pkCol = col
			}
		}
	}
	if t.pkCol == nil {
		t.logger.Fatal("Unknown PK")
	}

	t.indexName = t.schema.database.name
	if t.schema.name != "public" {
		t.indexName = t.schema.database.name + "_" + t.schema.name
	}

	if !t.pkCol.oldInWAL || (t.routingCol != nil && !t.routingCol.oldInWAL) {
		t.upsertOnly = true
	}
}

// Column gets (existing or default) column config.
func (t *Table) Column(name string) (col *Column) {
	if col, ok := t.columns[name]; ok {
		return col
	}

	col = &Column{
		table:     t,
		name:      name,
		fieldName: name,       // default
		index:     t.indexAll, // default
		connInfo:  t.schema.database.connInfo,
		logger:    t.logger.With(zap.String("column", name)),
	}
	t.columns[name] = col

	return col
}

// IndexColumns lists columns that are used for indexing, including inlines and id/routing fields
func (t *Table) IndexColumns() (columns []*Column) {
	for _, col := range t.columns {
		if col.index {
			columns = append(columns, col)
		}
		for _, inl := range t.isInlinedIn {
			for _, icol := range inl.columns {
				if col == icol {
					columns = append(columns, col)
					break
				}
			}
		}
	}
	// sort.Slice(columns, func(i, j int) bool { return columns[i].pos < columns[j].pos })
	return
}

func (t *Table) SelectQuery() string {
	var q strings.Builder
	q.WriteString(" SELECT ")

	for i, col := range t.IndexColumns() {
		if i != 0 {
			q.WriteByte(',')
		}
		col.pos = i // Update positions, relative to SELECT result. Position will be overwriten after replication starts

		q.WriteByte('"')
		q.WriteString(col.name) // TODO: escape
		q.WriteByte('"')
	}

	q.WriteString(" FROM ")
	q.WriteString(t.schema.name)
	q.WriteByte('.')
	q.WriteString(t.name)
	q.WriteByte(';')

	return q.String()
}
