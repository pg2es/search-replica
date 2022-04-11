package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pglogrepl"
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
	pos      int              // column position in LogicalReplication messages or `COPY TO ... ` query result, during cold start
	connInfo *pgtype.ConnInfo // same pointer as in database.connInfo

	value DecoderValue // TODO: decouple state from column

	logger *zap.Logger
}

func (c *Column) copy() *Column {
	newCol := *c
	newCol.value = pgtype.NewValue(c.value).(DecoderValue)
	return &newCol
}

func (col *Column) decode(data []byte, dataType uint8) (err error) {
	switch dataType {
	case pglogrepl.TupleDataTypeBinary:
		err = col.value.DecodeBinary(col.connInfo, data)
	case pglogrepl.TupleDataTypeText:
		err = col.value.DecodeText(col.connInfo, data)
	default:
		col.logger.Error("Unknown column dataType", zap.Uint8("type", dataType))
	}
	if err != nil {
		col.logger.Warn("failed to decode column value", zap.Error(err))
		return errors.Wrapf(err, "decode column (%s) value", col.name)
	}

	return nil
}

func (col *Column) MarshalJSON() ([]byte, error) {
	// reuse predefined MarshalJSON methods on postgress types, to preserve null values, skip zeroing, and possible speedup
	if marshaller, ok := col.value.(json.Marshaler); ok {
		val, err := marshaller.MarshalJSON()
		return val, errors.Wrapf(err, "Marshal column (%s) value via MarshalJSON", col.name)
	}

	val, err := json.Marshal(col.value.Get())
	return val, errors.Wrapf(err, "Marshal column (%s) value via json.Marshal", col.name)
}

func (c *Column) jsonKey() string {
	return c.fieldName
}

// setTyp sets column type, as decoder. In future will check if decoder is capable of binary decoding and json marshaling
func (c *Column) setTyp(typ *pgtype.DataType) {
	if typ == nil {
		c.logger.Error("undefined (nil) column type") // Consider Fatal.
		return
	}

	ok := false
	if c.value, ok = pgtype.NewValue(typ.Value).(DecoderValue); !ok { // own copy of type for column state
		c.logger.Error("can not set column type: DecodeValue is not implemented", zap.String("type", typ.Name)) // Consider Fatal.
	}
}

// DecoderValue can decode value and return pgtype object which implies Value interface.
type DecoderValue interface {
	pgtype.TextDecoder
	pgtype.BinaryDecoder // Not all types support BinaryDecoder.
	pgtype.Value
}

func (col *Column) string() string {
	if col.value.Get() == nil {
		return ""
	}
	return fmt.Sprint(col.value.Get())
}
