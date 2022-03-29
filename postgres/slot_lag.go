package postgres

import (
	"context"
	"fmt"

	_ "embed"

	"github.com/jackc/pgtype"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// lagQuery selects slot info. Current, committed positions and lag size.
//go:embed queries/get_slot_lag.sql
var lagQuery string

// Lag checks slot info via regular query connection
func (db *Database) Lag(ctx context.Context) error {
	db.queryConnMu.Lock()
	res := db.queryConn.ExecParams(ctx, lagQuery, [][]byte{[]byte(db.SlotName)}, nil, nil, []int16{binT, binT, binT, binT}).Read()
	db.queryConnMu.Unlock()

	if res.Err != nil {
		return errors.Wrap(res.Err, "fetch slot info")
	}
	if len(res.Rows) == 0 {
		return fmt.Errorf("slot %s does not exists", db.SlotName)
	}

	var (
		// current, committed, lag pgtype.Numeric
		current, committed, size string
		status                   pgtype.Text
	)

	row := res.Rows[0]
	db.connInfo.Scan(pgtype.TextOID, binT, row[0], &current)
	db.connInfo.Scan(pgtype.TextOID, binT, row[1], &committed)
	db.connInfo.Scan(pgtype.TextOID, binT, row[2], &size)
	db.connInfo.Scan(pgtype.TextOID, binT, row[3], &status)

	if status.Status == pgtype.Null || status.String == "lost" {
		db.logger.Error("slot is not usable; Recreate slot and reindex data.", zap.String("slot", db.SlotName), zap.String("slot_status", status.String))
	}
	db.logger.Info("slot lag",
		zap.String("slot", db.SlotName),
		zap.String("slot_status", status.String),
		zap.String("current_lsn", current),
		zap.String("committed_lsn", committed),
		zap.String("locked_wal_size", size),
	)

	return nil
}
