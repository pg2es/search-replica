package postgres

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pglogrepl"
	"github.com/pg2es/search-replica/postgres/pgcopy"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Selects all rown from table, and populates results into Database.results chanel.
// Copy existing data snapshoted by slot creation, using simple protocol.
func (t *Table) CopyAll(ctx context.Context, conn *pgconn.PgConn) error {
	t.init()

	q := t.copyQuery()
	t.logger.Info("COPYing snapshot", zap.String("sql", q))

	// buf := &bytes.Buffer{}
	pipeReader, pipeWriter := io.Pipe()
	wg := &sync.WaitGroup{}
	wg.Add(2)
	defer wg.Wait()

	parser := pgcopy.NewParser()
	go func() {
		err := parser.Parse(pipeReader)
		if err != nil {
			t.logger.Error("parser error", zap.Error(err))
		}
		wg.Done()
	}()

	go func() {
		cmd, err := conn.CopyTo(ctx, pipeWriter, q)
		defer pipeWriter.Close()
		if err != nil {
			t.logger.Error("copy to", zap.Error(err))
		}
		t.logger.Info("copy to CMD handler", zap.Int64("rows", cmd.RowsAffected()))
		wg.Done()
	}()

	// TODO: configurable
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()

	for {
		row, err := parser.Next(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "copy from")
		}

		err = t.decodeRow(row, pglogrepl.TupleDataTypeBinary)
		if err != nil {
			return errors.Wrap(err, "decode copy from")
		}

		if t.index {
			meta, _ := t.elasticBulkHeader(ESIndex)
			data, _ := t.MarshalJSON()
			t.schema.database.results <- Document{LSN: 0, Meta: meta, Data: data}
		}
		for _, inl := range t.isInlinedIn {
			meta, _ := inl.elasticBulkHeader(ESUpdate)
			data, _ := inl.jsonAddScript()
			t.schema.database.results <- Document{LSN: 0, Meta: meta, Data: data}
		}
	}
}

// Select everything and push (streaming) it into elasticsearch.
func (db *Database) Reindex(ctx context.Context) error {
	for _, table := range db.indexableTables() {
		if err := table.CopyAll(ctx, db.replConn); err != nil {
			return err
		}
	}
	return nil
}
