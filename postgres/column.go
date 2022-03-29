package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgtype"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type Column struct {
	table *Table // RO

	name      string // sql column name
	fieldName string // field name of json document. If empty, column name will be used
	sqlPK     bool   // in case if no config was provided, fallback to Postgres table primary key will be used
	index     bool   // specifies whether column should be indexed or ignored
	routing   bool   // is ES `_routing`
	oldInWAL  bool   // old value is stored in WAL for delete/update operations. See: https://www.postgresql.org/docs/10/sql-altertable.html#SQL-CREATETABLE-REPLICA-IDENTITY

	// Postgres
	pos      int              // column position in LogicalReplication messages or `SELECT ... ` query result, during cold start
	connInfo *pgtype.ConnInfo // same pointer as in database.connInfo
	decoder  DecoderValue
	// decoderBinary bool
	// decoderText   bool
	logger *zap.Logger
}

func (c *Column) jsonKey() string {
	return c.fieldName
}

func (old *Column) Copy() *Column {
	newCol := *old
	return &newCol
}

// Table returns column's owner table
func (cc *Column) Table() *Table {
	return cc.table
}

// setTyp sets column type, as decoder. In future will check if decoder is capable of binary decoding and json marshaling
func (c *Column) setTyp(typ *pgtype.DataType) {
	if typ == nil {
		c.logger.Error("undefined (nil) column type") // Consider Fatal.
		return
	}
	ok := false
	// XXX: split here for binary and text decoding
	// if c.binaryDecoder, ok = typ.Value.(BinaryDecoderValue); !ok {
	if c.decoder, ok = typ.Value.(DecoderValue); !ok {
		c.logger.Error("can not set column type: DecoderValue is not implemented", zap.String("type", typ.Name)) // Consider Fatal.
	}
}

// DecoderValue can decode value and return pgtype object which implies Value interface.
// see: https://github.com/kyleconroy/pgoutput/blob/6f49f4f3563fd90d1b96449f40540da7a0768f58/parse.go#L176-L179
type DecoderValue interface {
	pgtype.TextDecoder
	// pgtype.BinaryDecoder // Not all types support BinaryDecoder.
	Get() interface{} // From pgtype.Value
}

func (col *Column) checkRowBoundaries(row [][]byte) {
	if col.pos >= len(row) {
		col.logger.Fatal("column position is out of row range", zap.Int("position", col.pos), zap.Int("row_size", len(row)))
	}
}

func (col *Column) string(src []byte) string {
	if err := col.decoder.DecodeText(col.connInfo, src); err != nil {
		col.logger.Warn("failed to decode column value", zap.Error(err))
		return ""
	}
	if col.decoder.Get() == nil {
		return ""
	}
	return fmt.Sprint(col.decoder.Get())
}

// stringFromRow is used for _id, routing, join parent fields, where string value is required.
func (col *Column) stringFromRow(row [][]byte) string {
	col.checkRowBoundaries(row)
	return col.string(row[col.pos])
}

func (col *Column) jsonFromRow(row [][]byte) ([]byte, error) {
	col.checkRowBoundaries(row)
	return col.json(row[col.pos])
}

// json returns json encoded value of column in src
func (col *Column) json(src []byte) ([]byte, error) {
	if err := col.decoder.DecodeText(col.connInfo, src); err != nil {
		col.logger.Warn("failed to decode column value", zap.Error(err))
		return nil, errors.Wrapf(err, "decode column (%s) value", col.name)
	}

	// reuse predefined MarshalJSON methods on postgress types, to preserve null values, skip zeroing, and possible speedup
	if marshaller, ok := col.decoder.(json.Marshaler); ok {
		val, err := marshaller.MarshalJSON()
		return val, errors.Wrapf(err, "Marshal column (%s) value via MarshalJSON", col.name)
	}

	val, err := json.Marshal(col.decoder.Get())
	return val, errors.Wrapf(err, "Marshal column (%s) value via json.Marshal", col.name)

}
