package postgres

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/jackc/pgconn"
	"github.com/jackc/pglogrepl"
	"github.com/pg2es/search-replica/postgres/pgcopy"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var (
	metricCopyRows = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "copy_messages",
		Help: "Initial rows received via COPY",
	}, []string{"table"})
)

func init() {
	prometheus.MustRegister(metricCopyRows)
}

// Selects all rown from table, and populates results into Database.results chanel.
// Copy existing data snapshoted by slot creation, using simple protocol.
func (t *Table) CopyAll(ctx context.Context, conn *pgconn.PgConn) error {
	t.init()

	// XXX: ctx.WithDeadline here can lead to deadlock.

	q := t.copyQuery()
	t.logger.Info("COPYing snapshot", zap.String("sql", q))

	pipeReader, pipeWriter := io.Pipe()
	wg := &sync.WaitGroup{}

	wg.Add(2)
	defer wg.Wait()

	parser := pgcopy.NewParser()
	go func() {
		defer wg.Done()
		err := parser.Parse(pipeReader)
		if err != nil {
			t.logger.Error("parser error", zap.Error(err))
		}
	}()

	go func() {
		defer wg.Done()
		defer pipeWriter.Close()
		cmd, err := conn.CopyTo(ctx, pipeWriter, q)
		if err != nil {
			t.logger.Error("copy to", zap.Error(err))
		}
		t.logger.Info("copied to CMD handler", zap.Int64("rows", cmd.RowsAffected()))
	}()

	tableRows := metricCopyRows.WithLabelValues(t.name)
	stream := t.schema.database.stream // shortcut

	for {
		row, err := parser.Next(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("copy from: %w", err)
		}
		tableRows.Inc()

		err = t.decodeRow(row, pglogrepl.TupleDataTypeBinary)
		if err != nil {
			return fmt.Errorf("decode copy from: %w", err)
		}

		if t.index {
			meta, _ := t.elasticBulkHeader(ESIndex)
			data, _ := t.MarshalJSON()
			stream.add(Document{Meta: meta, Data: data})
		}
		for _, inl := range t.isInlinedIn {
			meta, _ := inl.elasticBulkHeader(ESUpdate)
			data, _ := inl.jsonAddScript()
			stream.add(Document{Meta: meta, Data: data})
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
