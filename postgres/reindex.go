package postgres

import (
	"context"
	"log"

	"github.com/jackc/pgconn"
	"github.com/pkg/errors"
)

// Selects all rown from table, and populates results into Database.results chanel.
// Should never be used concurently with other tables, since they share same connection
// NiceToHave: connection pool
func (t *Table) SelectAll(ctx context.Context, conn *pgconn.PgConn) error {
	t.init()

	q := t.SelectQuery()
	log.Printf("Selecting everyting from %s, with `%s`", t.name, q)
	// separate connection, since it needs to stream result, reading whole table
	// todo, use binary decode here
	res := conn.ExecParams(ctx, q, nil, nil, nil, nil)

	// Set col types, since, we haven't received
	// a relation message from replication yet
	for _, col := range t.IndexColumns() {
		dataType, err := t.schema.database.DataType(res.FieldDescriptions()[col.pos].DataTypeOID)
		if err != nil {
			return errors.Wrapf(err, "setup column type %s.%s", col.table.name, col.name)
		}
		col.setTyp(dataType)
	}

	for res.NextRow() {
		row := res.Values()

		// TODO error check or logging
		if t.index {
			meta, _ := t.elasticBulkHeader(ESIndex, row)
			data, _ := t.EncodeRowJSON(row)
			t.schema.database.results <- Document{LSN: 0, Meta: meta, Data: data}
		}
		for _, inl := range t.isInlinedIn {
			meta, _ := inl.elasticBulkHeader(ESUpdate, row)
			data, _ := inl.jsonAddScript(row)
			t.schema.database.results <- Document{LSN: 0, Meta: meta, Data: data}
		}
	}

	if _, err := res.Close(); err != nil {
		return errors.Wrap(err, "close select result")
	}

	return nil
}
