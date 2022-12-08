package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgproto3/v2"
	"github.com/jackc/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const outputPlugin = "pgoutput" // important
const defaultApplicationName = "PG2ES/SearchReplica"

var (
	metricMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "streaming_messages",
		Help: "Wall decoded messages received in streaming replication",
	}, []string{"operation", "table"})
)

func init() {
	prometheus.MustRegister(metricMessages)
}

func pgconnConfig() (*pgconn.Config, error) {
	config, err := pgconn.ParseConfig("") // falback to parsing env variables
	if err != nil {
		return nil, fmt.Errorf("can not parse Postgres config: %w", err)
	}
	if config.Database == "" {
		return nil, errors.New("database needs to be specified")
	}

	return config, nil
}

// Connect makes two connections to DB for discovery and replication
func (db *Database) Connect(ctx context.Context) error {
	config, err := pgconnConfig()
	if err != nil {
		return err
	}
	db.name = config.Database // By default database name == index name

	config.RuntimeParams["application_name"] = defaultApplicationName
	config.RuntimeParams["options"] = "-c statement_timeout=0" // Equivalent of `SET statement_timeout=0;`

	// "replication" key should not be defined for regular (query) connection.
	// this connection is required for config and type discovery.
	// In replication mode, only the simple query protocol can be used, which is not sufficient in this case.
	delete(config.RuntimeParams, "replication")
	if db.queryConn, err = pgconn.ConnectConfig(ctx, config); err != nil {
		return fmt.Errorf("can not connect: %w", err)
	}

	// See: https://www.postgresql.org/docs/14/libpq-connect.html#LIBPQ-CONNECT-REPLICATION
	// Following parameter switch connection into logical replication mode.
	config.RuntimeParams["replication"] = "database"
	if db.replConn, err = pgconn.ConnectConfig(ctx, config); err != nil {
		return fmt.Errorf("can not connect: %w", err)
	}

	db.version = db.replConn.ParameterStatus("server_version")

	// check if Binary decoding is possible (PG14+)
	major, err := strconv.Atoi(strings.Split(db.version, ".")[0])
	if err != nil {
		db.logger.Warn("can not parse Postgres major version", zap.String("postgres_version", db.version))
	}
	db.useBinary = major >= 14

	db.logger.Info("Connected to Database", zap.String("postgres_version", db.version), zap.Bool("binary_streaming", db.useBinary))
	return nil
}

func (db *Database) Close(ctx context.Context) error {
	db.queryConnMu.Lock()
	db.queryConn.Close(ctx)
	db.queryConnMu.Unlock()

	db.replConn.Close(ctx)
	return nil
}

// CreateReplicationSlot creates a replication slot at current position and uses newly created snapshot in current transaction.
// For the sake of consistency it's important to use this method and initial data copying within transaction
// db.Tx(ctx)
// db.CreateReplicationSlot(ctx)
// ... copy data
// db.Commit(ctx)
func (db *Database) CreateReplicationSlot(ctx context.Context) {
	opts := pglogrepl.CreateReplicationSlotOptions{
		Temporary:      false,
		SnapshotAction: "USE_SNAPSHOT",
		Mode:           pglogrepl.LogicalReplication,
	}
	_, err := pglogrepl.CreateReplicationSlot(ctx, db.replConn, db.SlotName, outputPlugin, opts)
	if err != nil {
		db.logger.Error("failed to create replication slot", zap.String("slot", db.SlotName), zap.Error(err))
		return
	}
	db.logger.Info("created replication slot", zap.String("slot", db.SlotName))
}

func (db *Database) Tx(ctx context.Context) error {
	return db.replConn.Exec(ctx, "BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ").Close()
}

func (db *Database) Commit(ctx context.Context) error {
	return db.replConn.Exec(ctx, "COMMIT").Close()
}

func (db *Database) DropReplicationSlot(ctx context.Context) {
	opts := pglogrepl.DropReplicationSlotOptions{Wait: true} // true?
	if err := pglogrepl.DropReplicationSlot(ctx, db.replConn, db.SlotName, opts); err != nil {
		db.logger.Error("failed to drop replication slot", zap.String("slot", db.SlotName), zap.Error(err))
	}
	db.logger.Info("dropped replication slot", zap.String("slot", db.SlotName))
}

// StartReplication switches replConn into `CopyBoth` mode and starts streaming.
// decoded logical messages are passed for further processing, while status updates happens here.
// during the streaming, replConn is locked and can not be used for anything else.
//
// XXX: PrimaryKeepaliveMessage.ServerWALEnd is actually the location up to which the WAL is sent
// See: https://stackoverflow.com/questions/71016200/proper-standby-status-update-in-streaming-replication-protocol
func (db *Database) StartReplication(ctx context.Context, at pglogrepl.LSN) error {
	pluginArguments := []string{
		"proto_version '1'",                          // next protocol versions are not required for our use case.
		"publication_names '" + db.Publication + "'", // TODO: escape pgPublication; support multiple publications
	}
	if db.useBinary { // Binary streaming for PG14+
		pluginArguments = append(pluginArguments, "binary 'true'")
	}

	opts := pglogrepl.StartReplicationOptions{PluginArgs: pluginArguments}
	if err := pglogrepl.StartReplication(ctx, db.replConn, db.SlotName, at, opts); err != nil {
		return fmt.Errorf("start replication: %w", err)
	}

	standbyDeadline := time.Now().Add(db.StandbyTimeout)
	db.logger.Info("Started streaming replication")

	prevCommit := db.stream.Position()
	for {
		// TODO: exit on <- ctx.Done()
		if time.Now().After(standbyDeadline) {
			commit := db.stream.Position()
			status := pglogrepl.StandbyStatusUpdate{WALWritePosition: commit}
			err := pglogrepl.SendStandbyStatusUpdate(ctx, db.replConn, status)
			if err != nil { // TODO: add few retries
				return fmt.Errorf("send standby status update: %w", err)
			}
			if commit > prevCommit {
				prevCommit = commit
				db.logger.Debug("Committed LSN", zap.Stringer("lsn", commit))
			}
			standbyDeadline = time.Now().Add(db.StandbyTimeout)
		}

		// be careful with ctx shadowing
		ctxWithDeadline, stopDeadlineTimer := context.WithDeadline(ctx, standbyDeadline)
		msg, err := db.replConn.ReceiveMessage(ctxWithDeadline)
		stopDeadlineTimer()

		if err != nil {
			if pgconn.Timeout(err) {
				select {
				case <-ctx.Done():
					db.logger.Info("shutdown: streaming stopped")
					if commit := db.stream.Position(); commit > prevCommit {
						status := pglogrepl.StandbyStatusUpdate{WALWritePosition: commit}
						ctxWithTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						err := pglogrepl.SendStandbyStatusUpdate(ctxWithTimeout, db.replConn, status)
						db.logger.Debug("shutdown: committed latest position", zap.String("lsn", commit.String()), zap.Error(err))
					}

					return nil // graceful exit
				default: // non blocking continue
				}
				continue // Deadline to do a standby status update
			}
			// maybe <- ctx.Done() would work here
			db.logger.Fatal("failed to receive message", zap.Error(err))
		}

		switch msg := msg.(type) {
		case *pgproto3.CopyData:
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
				if err != nil {
					db.logger.Fatal("failed to parse PrimaryKeepaliveMessage", zap.Error(err))
				}
				if pkm.ReplyRequested {
					standbyDeadline = time.Time{}
				}
				db.stream.add(Position(pkm.ServerWALEnd))

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					db.logger.Fatal("failed to parse XLogData", zap.Error(err))
				}
				logicalMsg, err := pglogrepl.Parse(xld.WALData)
				if err != nil {
					db.logger.Fatal("failed to parse replication message from XLogData", zap.Error(err))
				}
				// check xld.ServerWALEnd instead xld.WALStart
				db.HandleLogical(ctx, xld.WALStart, logicalMsg) // TODO: make it non-blocking for standby
			}
		default:
			db.logger.Fatal("received unexpected message", zap.String("type", fmt.Sprintf("%T", msg)))
		}

	}
}

// must is temporarily helper to fail in some places
func must(data []byte, err error) []byte {
	if err != nil {
		log.Fatal(err)
	}
	return data
}

func (db *Database) HandleLogical(ctx context.Context, lsn pglogrepl.LSN, msg pglogrepl.Message) error {
	pos := Position(lsn)
	switch v := msg.(type) {
	case *pglogrepl.BeginMessage:
	case *pglogrepl.CommitMessage:
		// Nice to have some lock, to have whole transaction in single ES batch
		// d.results <- Document{LSN: lsn}

	// This message is delivered at the beginning, and after table schema changes.
	// TODO: support dropped columns
	case *pglogrepl.RelationMessage:
		table := db.schema(v.Namespace).table(v.RelationName)
		table.SetRelationID(v.RelationID)
		metricMessages.WithLabelValues("metadata", table.name).Inc()

		for pos, relcol := range v.Columns {
			col := table.Column(relcol.Name)
			// ... relcol.Flags&1 != 0  means PK
			col.pos = pos

			dataType, err := db.dataTypeDecoder(relcol.DataType)
			if err != nil {
				return fmt.Errorf("setup column type %s.%s: %w", col.table.name, col.name, err)
			}
			col.setTyp(dataType)
		}
		table.init() // field names

	case *pglogrepl.InsertMessage:
		table := db.relation(v.RelationID)
		metricMessages.WithLabelValues("insert", table.name).Inc()

		table.decodeTuple(v.Tuple)
		if table.index {
			meta := must(table.elasticBulkHeader(ESIndex))
			data := must(table.MarshalJSON())
			db.stream.add(Document{Position: pos, Meta: meta, Data: data})
		}

		for _, inl := range table.isInlinedIn {
			meta, _ := inl.elasticBulkHeader(ESUpdate)
			data, _ := inl.jsonAddScript()
			db.stream.add(Document{Position: pos, Meta: meta, Data: data})
		}

	case *pglogrepl.UpdateMessage:
		table := db.relation(v.RelationID)
		metricMessages.WithLabelValues("update", table.name).Inc()

		// IF document keys (_id, _routing) changed, we can't update it, thus it needs to be re-created.

		insert := false
		if !table.upsertOnly {
			table.decodeTuple(v.OldTuple)

			if table.index && table.tupleKeysChanged(v.OldTuple, v.NewTuple) {
				insert = true // new document would be inserted
				// but we need to delete current document first
				meta := must(table.elasticBulkHeader(ESDelete))
				db.stream.add(Document{Position: pos, Meta: meta})
			}

			// Clean up old inlines
			for _, inl := range table.isInlinedIn {
				if inl.tupleKeysChanged(v.OldTuple, v.NewTuple) {
					meta := must(inl.elasticBulkHeader(ESUpdate))
					data := must(inl.jsonDelScript())
					db.stream.add(Document{Position: pos, Meta: meta, Data: data})
				}
			}
		}

		table.decodeTuple(v.NewTuple)

		if table.index {
			if insert { // create new document, since we deleted previous
				meta := must(table.elasticBulkHeader(ESInsert))
				data := must(table.MarshalJSON())
				db.stream.add(Document{Position: pos, Meta: meta, Data: data})
			} else { // update existing
				// XXX: ESUpdate is correct here, and would work fine assuming that data is consistent.
				meta := must(table.elasticBulkHeader(ESUpdate))
				data := must(table.EncodeUpdateRowJSON())
				db.stream.add(Document{Position: pos, Meta: meta, Data: data})
			}
		}

		for _, inl := range table.isInlinedIn {
			meta := must(inl.elasticBulkHeader(ESUpdate))
			data := must(inl.jsonAddScript())
			db.stream.add(Document{Position: pos, Meta: meta, Data: data})
		}

	case *pglogrepl.DeleteMessage:
		table := db.relation(v.RelationID)
		metricMessages.WithLabelValues("delete", table.name).Inc()
		table.decodeTuple(v.OldTuple)

		if table.index && !table.upsertOnly {
			meta := must(table.elasticBulkHeader(ESDelete))
			db.stream.add(Document{Position: pos, Meta: meta})
		}

		for _, inl := range table.isInlinedIn {
			if inl.upsertOnly {
				continue
			}
			meta := must(inl.elasticBulkHeader(ESUpdate))
			data := must(inl.jsonDelScript())
			db.stream.add(Document{Position: pos, Meta: meta, Data: data})
		}

	case *pglogrepl.OriginMessage:
		// skip. Useless for our case

	// Received to inform us about UserDefined types
	case *pglogrepl.TypeMessage:
		db.discoverUnknownType(ctx, pgtype.OID(v.DataType))

	// case *pglogrepl.TruncateMessage:
	// should be logged as non-implemented type in `default` closure.
	default:
		db.logger.Warn(
			"message type is not implemented",
			zap.Stringer("type_name", msg.Type()),
			zap.Uint8("type_code", uint8(msg.Type())),
		)
		return fmt.Errorf("unknown logical message type %s", msg.Type())
	}
	return nil
}
