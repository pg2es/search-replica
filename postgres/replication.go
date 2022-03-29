package postgres

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgproto3/v2"
	"github.com/jackc/pgtype"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const OutputPlugin = "pgoutput" // important

func pgconnConfig() (*pgconn.Config, error) {
	config, err := pgconn.ParseConfig("") // falback to parsing env variables
	if err != nil {
		return nil, errors.Wrap(err, "can not parse postgress config")
	}
	if config.Database == "" {
		return nil, errors.New("database needs to be specified")
	}

	return config, nil
}

// Connect makes two connections to DB for discovery and replication
func (d *Database) Connect(ctx context.Context) error {
	config, err := pgconnConfig()
	if err != nil {
		return err
	}
	d.name = config.Database // By default database name == index name

	// "replication" key should not be defined for regular (query) connection.
	// this connection is required for discovery and initial feetching data
	// Since in physical or logical replication mode, only the simple query protocol can be used, which is not sufficient in this case.
	delete(config.RuntimeParams, "replication")
	if d.queryConn, err = pgconn.ConnectConfig(ctx, config); err != nil {
		return errors.Wrap(err, "can not connect")
	}

	// See: https://www.postgresql.org/docs/14/libpq-connect.html#LIBPQ-CONNECT-REPLICATION
	// Following parameter switch connection into logical replication mode.
	config.RuntimeParams["replication"] = "database"
	if d.replConn, err = pgconn.ConnectConfig(ctx, config); err != nil {
		return errors.Wrap(err, "can not connect")
	}

	log.Printf("Connected to DB %s", d.name)

	return nil
}

// newConn for `select *` queries. ( for -reindex ). Do not forget to close it.
func (d *Database) newConn(ctx context.Context) (conn *pgconn.PgConn, err error) {
	config, _ := pgconnConfig()
	delete(config.RuntimeParams, "replication")
	conn, err = pgconn.ConnectConfig(ctx, config)

	return conn, errors.Wrap(err, "can not connect")
}

func (db *Database) Close(ctx context.Context) error {
	// if d.queryConn != nil {

	db.queryConnMu.Lock()
	db.queryConn.Close(ctx)
	db.queryConnMu.Unlock()

	db.replConn.Close(ctx)
	return nil
}

func (db *Database) CreateReplicationSlot(ctx context.Context) {
	opts := pglogrepl.CreateReplicationSlotOptions{Temporary: false}
	if _, err := pglogrepl.CreateReplicationSlot(ctx, db.replConn, db.SlotName, OutputPlugin, opts); err != nil {
		db.logger.Error("failed to create replication slot", zap.String("slot", db.SlotName), zap.Error(err))
	}
	db.logger.Info("created replication slot", zap.String("slot", db.SlotName))
}

func (db *Database) DropReplicationSlot(ctx context.Context) {
	opts := pglogrepl.DropReplicationSlotOptions{Wait: true} // true?
	if err := pglogrepl.DropReplicationSlot(ctx, db.replConn, db.SlotName, opts); err != nil {
		db.logger.Error("failed to drop replication slot", zap.String("slot", db.SlotName), zap.Error(err))
	}
	db.logger.Info("dropped replication slot", zap.String("slot", db.SlotName))
}

func (db *Database) GetCurrentLSN(ctx context.Context) pglogrepl.LSN {
	sysident, err := pglogrepl.IdentifySystem(ctx, db.replConn)
	if err != nil {
		db.logger.Fatal("failed to identify system", zap.Error(err))
	}
	db.logger.Info("system info",
		zap.String("SystemID", sysident.SystemID),
		zap.Int32("Timeline", sysident.Timeline),
		zap.String("XLogPos", sysident.XLogPos.String()), // current WAL position
		zap.String("DBName", sysident.DBName),
	)
	return sysident.XLogPos
}

func (db *Database) StartReplication(ctx context.Context, at pglogrepl.LSN) error {
	pluginArguments := []string{
		"proto_version '1'",
		"publication_names '" + db.Publication + "'", // TODO: escape pgPublication
		// "binary 'true'" 							  // TODO: Add binary for PG14+
	}

	opts := pglogrepl.StartReplicationOptions{PluginArgs: pluginArguments}
	if err := pglogrepl.StartReplication(ctx, db.replConn, db.SlotName, at, opts); err != nil {
		errors.Wrap(err, "start replication")
	}

	standbyDeadline := time.Now().Add(db.StandbyTimeout)
	db.logger.Info("started streaming replication")

	prevCommit := db.LSN()
	for {
		if time.Now().After(standbyDeadline) {
			commit := db.LSN()
			status := pglogrepl.StandbyStatusUpdate{WALWritePosition: commit}
			err := pglogrepl.SendStandbyStatusUpdate(ctx, db.replConn, status)
			if err != nil { // TODO: add few retries
				return errors.Wrap(err, "send standby status update")
			}
			if commit > prevCommit {
				prevCommit = commit
				db.logger.Info("Commited LSN", zap.String("lsn", commit.String()))
			}
			standbyDeadline = time.Now().Add(db.StandbyTimeout)
		}

		ctx, stopDeadlineTimer := context.WithDeadline(ctx, standbyDeadline)
		msg, err := db.replConn.ReceiveMessage(ctx)
		stopDeadlineTimer()

		if err != nil {
			if pgconn.Timeout(err) {
				continue // Deadline to do a standby status update
			}
			db.logger.Fatal("failed to receive message", zap.Error(err))
		}

		WALPos := pglogrepl.LSN(0)

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
				if WALPos < pkm.ServerWALEnd {
					WALPos = pkm.ServerWALEnd
					db.results <- Document{LSN: pkm.ServerWALEnd}
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					db.logger.Fatal("failed to parse XLogData", zap.Error(err))
				}
				logicalMsg, err := pglogrepl.Parse(xld.WALData)
				if err != nil {
					db.logger.Fatal("failed to parse replication message from XLogData", zap.Error(err))
				}
				// Not sure what exactly to commit. Docs are saying "The location of the last WAL byte + 1...",
				// We have start, length of data and `+1`, and some combinations of those is correct.
				// however pg_basebackup/pg_recvlogical.c just sets it to latest WALStart (see `output_written_lsn`)
				// For now, let's stick with single WALStart
				if xld.WALStart < WALPos { // TODO: Clarify in Postgres Slack and remove
					db.logger.Fatal("KeepAlive and Data messages are not ordered by LSN")
				}
				db.HandleLogical(ctx, xld.WALStart, logicalMsg) // TODO: make it non-blocking for standby
				if WALPos < xld.WALStart {
					WALPos = xld.WALStart
				}
			}
		default:
			db.logger.Fatal("received unexpected message", zap.String("type", fmt.Sprintf("%T", msg)))
		}

	}

	return nil
}

// must is temporarily helper to fail in some places
func must(data []byte, err error) []byte {
	if err != nil {
		log.Fatal(err)
	}
	return data
}

func (db *Database) HandleLogical(ctx context.Context, lsn pglogrepl.LSN, msg pglogrepl.Message) error {
	switch v := msg.(type) {
	case *pglogrepl.BeginMessage:
	case *pglogrepl.CommitMessage:
		// Nice to have some lock, to have whole transaction in single ES batch
		// d.results <- Document{LSN: lsn}

	// This message is delivered at the bedinning, and after table schema canges.
	// TODO: support droped columns
	case *pglogrepl.RelationMessage:
		table := db.Schema(v.Namespace).Table(v.RelationName)
		table.SetRelationID(v.RelationID)
		for pos, relcol := range v.Columns {
			col := table.Column(relcol.Name)
			// ... relcol.Flags&1 != 0  means PK
			col.pos = pos

			dataType, err := db.DataType(relcol.DataType)
			if err != nil {
				return errors.Wrapf(err, "setup column type %s.%s", col.table.name, col.name)
			}
			col.setTyp(dataType)
		}
		table.init() // field names

	case *pglogrepl.InsertMessage:
		table := db.Relation(v.RelationID)
		row := rowFromPGTuple(v.Tuple)

		if table.index {
			meta := must(table.elasticBulkHeader(ESIndex, row))
			data := must(table.EncodeRowJSON(row))
			db.results <- Document{LSN: lsn, Meta: meta, Data: data}
		}

		for _, inl := range table.isInlinedIn {
			meta, _ := inl.elasticBulkHeader(ESUpdate, row)
			data, _ := inl.jsonAddScript(row)
			db.results <- Document{LSN: lsn, Meta: meta, Data: data}
		}

	case *pglogrepl.UpdateMessage:
		table := db.Relation(v.RelationID)
		// TODO: If PK or Routing field was changed, procceed with delete & insert pair. Also checking for table.insertOnly.
		// if !table.insertOnly && () {
		// oldRow := rowFromPGTuple(v.OldTuple)
		// if table.keysChanged(oldRow, row)
		// }
		row := rowFromPGTuple(v.NewTuple)

		// XXX: ESUpdate is correct here, and would work fine assuming that data is consistent.
		if table.index {
			meta := must(table.elasticBulkHeader(ESUpdate, row))
			data := must(table.EncodeUpdateRowJSON(row))
			db.results <- Document{LSN: lsn, Meta: meta, Data: data}
		}

		for _, inl := range table.isInlinedIn {
			meta := must(inl.elasticBulkHeader(ESUpdate, row))
			data := must(inl.jsonAddScript(row))
			db.results <- Document{LSN: lsn, Meta: meta, Data: data}
		}

	case *pglogrepl.DeleteMessage:
		table := db.Relation(v.RelationID)
		row := rowFromPGTuple(v.OldTuple)

		if table.index && !table.upsertOnly {
			meta := must(table.elasticBulkHeader(ESDelete, row))
			db.results <- Document{LSN: lsn, Meta: meta}
		}

		for _, inl := range table.isInlinedIn {
			if inl.upsertOnly {
				continue
			}
			meta := must(inl.elasticBulkHeader(ESUpdate, row))
			data := must(inl.jsonDelScript(row))
			db.results <- Document{LSN: lsn, Meta: meta, Data: data}
		}

	case *pglogrepl.TruncateMessage:
		// TODO: Delete and recreate index, while preserving mapping.
		log.Printf("not implemented message %s", msg.Type())

	case *pglogrepl.OriginMessage:
		// skip. Useless for our case

	// Received to inform us about UserDefined types
	case *pglogrepl.TypeMessage:
		log.Printf("Type Message %s.%s OID: %d", v.Namespace, v.Name, v.DataType)
		db.discoverUnknownType(ctx, pgtype.OID(v.DataType))
	default:
		log.Printf("unknown message %s", msg.Type())
		return fmt.Errorf("unknown logical message type %s", msg.Type())
	}
	return nil
}
