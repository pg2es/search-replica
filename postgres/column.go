package postgres

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgtype"
	"go.uber.org/zap"
)

type Column struct {
	table *Table

	name      string // sql column name
	fieldName string // field name of json document. If empty, column name will be used
	sqlPK     bool   // in case if no config was provided, fallback to Postgres table primary key will be used
	index     bool   // specifies whether column should be indexed or ignored
	oldInWAL  bool   // old value is stored in WAL for delete/update operations. See: https://www.postgresql.org/docs/10/sql-altertable.html#SQL-CREATETABLE-REPLICA-IDENTITY

	// Postgres
	pos      int              // column position in LogicalReplication messages or `COPY TO ... ` query result, during cold start
	connInfo *pgtype.ConnInfo // same pointer as in database.connInfo

	value     DecoderValue // TODO: decouple state from column
	valueOmit bool         // TODO: merge with value

	logger *zap.Logger
}

func (col *Column) decode(data []byte, dataType uint8) (err error) {
	col.valueOmit = false
	switch dataType {
	case pglogrepl.TupleDataTypeBinary:
		err = col.value.DecodeBinary(col.connInfo, data)
	case pglogrepl.TupleDataTypeText:
		err = col.value.DecodeText(col.connInfo, data)
	case pglogrepl.TupleDataTypeNull, pglogrepl.TupleDataTypeToast:
		col.valueOmit = true
	default:
		col.valueOmit = true
		col.logger.Error("Unknown column dataType", zap.String("type", strconv.QuoteRune(rune(dataType))))
	}
	if err != nil {
		col.logger.Warn("failed to decode column value", zap.Error(err))
		return fmt.Errorf("decode column (%s) value: %w", col.name, err)
	}

	return nil
}

func (col *Column) Omit() bool {
	return col.valueOmit
}

func (col *Column) MarshalJSON() ([]byte, error) {
	// reuse predefined MarshalJSON methods on Postgres types, to preserve null values, skip zeroing, and possible speedup
	if marshaller, ok := col.value.(json.Marshaler); ok {
		val, err := marshaller.MarshalJSON()
		if err != nil {
			return val, fmt.Errorf("marshal column (%s) value via MarshalJSON: %w", col.name, err)
		}
		return val, nil
	}

	val, err := json.Marshal(col.value.Get())
	if err != nil {
		return val, fmt.Errorf("marshal column (%s) value via json.Marshal: %w", col.name, err)
	}
	return val, nil
}

func (c *Column) jsonKey() string {
	// TODO: fix for omitted values?
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
