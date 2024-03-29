package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pglogrepl"
	jwriter "github.com/mailru/easyjson/jwriter"
	"go.uber.org/zap"
)

var (
	// ErrColumnOutOfRange means that received result tuple is smaller than expected column position
	ErrColumnOutOfRange = errors.New("column out of range")
)

type Table struct {
	schema  *Schema
	columns map[string]*Column // all columns by name

	inlines     []*Inline // inline name -> table which uses it. owns
	isInlinedIn []*Inline // list of tables, this table `is inlined in`; We need to update in ES documents of those tables.

	name  string
	relID uint32 // used in logical_replication protocol

	docType string // name of document type within index

	index      bool
	indexAll   bool // index all columns by default
	upsertOnly bool // without old PKs / _routing in WAL, proper update & delete is impossible
	tagParsed  bool

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

// tupleKeysChanged tells whether document needs to be recreated
func (t *Table) tupleKeysChanged(oldTuple, newTuple *pglogrepl.TupleData) bool {
	if oldTuple == nil {
		return false
	}

	if !bytes.Equal(
		oldTuple.Columns[t.pkCol.pos].Data,
		newTuple.Columns[t.pkCol.pos].Data,
	) {
		return true
	}

	if t.routingCol != nil && !bytes.Equal(
		newTuple.Columns[t.routingCol.pos].Data,
		oldTuple.Columns[t.routingCol.pos].Data,
	) {
		return true
	}

	return false
}

func (t *Table) decodeRow(row [][]byte, dataType uint8) error {
	var empty bool
	for _, col := range t.indexColumns() {
		if err := col.decode(row[col.pos], dataType); err != nil {
			return err
		}
		empty = empty || col.valueOmit
	}
	if empty {
		t.logger.Debug("Some columns are empty and may be omitted from resulting JSON")
	}
	return nil
}

func (t *Table) decodeTuple(tuple *pglogrepl.TupleData) error {
	var empty bool
	for _, col := range t.indexColumns() {
		if col.pos >= len(tuple.Columns) {
			return ErrColumnOutOfRange
		}
		if err := col.decode(tuple.Columns[col.pos].Data, tuple.Columns[col.pos].DataType); err != nil {
			return err
		}
		empty = empty || col.valueOmit
	}
	if empty {
		t.logger.Debug("Some columns are empty and may be omitted from resulting JSON")
	}
	return nil
}

func (t *Table) elasticBulkHeader(action ESAction) ([]byte, error) {
	header := bulkHeader{
		Action: action,
		Index:  t.indexName,
		ID:     t.pkCol.string(),
	}

	if !t.pkNoPrefix { // add document type prefix to ID, to avoid collisions
		header.ID = t.name + "_" + header.ID
	}
	if t.routingCol != nil {
		header.Routing = t.routingCol.string()
	}

	return json.Marshal(header)
}

func (t *Table) MarshalJSON() ([]byte, error) {
	out := jwriter.Writer{}
	t.jsonEncodeRow(&out)
	return out.Buffer.BuildBytes(), out.Error
}

// EncodeUpdateRowJSON wraps row into `{"doc": ... }` object, required by ElasticSearch bulk request syntax for update queries
func (t *Table) EncodeUpdateRowJSON() ([]byte, error) {
	out := jwriter.Writer{}
	out.RawString(`{"doc":`)

	t.jsonEncodeRow(&out)

	out.RawByte('}')
	return out.Buffer.BuildBytes(), out.Error
}

func (t *Table) jsonEncodeRow(buf *jwriter.Writer) {
	doc := document{}
	for _, col := range t.columns { // add real columns
		if col.index {
			doc.fields = append(doc.fields, col)
		}
	}
	if t.join.enabled {
		doc.fields = append(doc.fields, &t.join)
	}
	doc.fields = append(doc.fields, stringKV{key: "docType", value: t.docType})

	doc.MarshalEasyJSON(buf)
}

// init: consistency checks and pre-encode caching
func (t *Table) init() {
	if t.pkCol == nil {
		t.logger.Debug("PK is not configured. Fallback to implicit PK")
		for _, col := range t.columns {
			if col.sqlPK {
				t.pkCol = col
			}
		}
	}

	for _, inl := range t.isInlinedIn {
		inl.init()
	}

	if !t.index { // Skip checks & setup for ignored tables
		return
	}

	if t.pkCol == nil {
		t.logger.Fatal("Unknown PK")
	}

	// TODO: Index name
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

// indexColumns lists columns that are used for indexing, including inlines and id/routing fields
func (t *Table) indexColumns() (columns []*Column) {
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

// copyQuery returns copy query suitable for initial data load.
// E.G: COPY "foo" ("baz","baz") TO STDOUT WITH BINARY;
func (t *Table) copyQuery() string {
	var q strings.Builder
	q.WriteString(`COPY `)
	q.WriteByte('"')
	q.WriteString(strings.ReplaceAll(t.schema.name, `"`, `""`))
	q.WriteString(`"."`)
	q.WriteString(strings.ReplaceAll(t.name, `"`, `""`))
	q.WriteString(`" (`)

	for i, col := range t.indexColumns() {
		if i != 0 {
			q.WriteByte(',')
		}
		col.pos = i // Update positions, relative to COPY result. Position will be overwritten after replication starts

		q.WriteByte('"')
		q.WriteString(strings.ReplaceAll(col.name, `"`, `""`))
		q.WriteByte('"')
	}
	q.WriteByte(')')
	q.WriteString(` TO STDOUT WITH BINARY;`)

	return q.String()
}
