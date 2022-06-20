package postgres

import (
	"context"
	"math"
	"time"

	_ "embed"

	"github.com/jackc/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func (db *Database) RegisterSlotLagMetric(ctx context.Context) {
	metricSlotLag := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "slot_lag",
		Help: "how much bytes do we need to read, to keep up with current DB state",
	}, func() (lag float64) {
		timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		db.queryConnMu.Lock()
		res := db.queryConn.ExecParams(timeout, `
			SELECT pg_current_wal_lsn() - confirmed_flush_lsn AS lag
			FROM pg_replication_slots WHERE slot_name=$1;
		`, [][]byte{[]byte(db.SlotName)}, nil, nil, []int16{binT}).Read()
		db.queryConnMu.Unlock()
		if res.Err != nil || len(res.Rows) == 0 {
			db.logger.Warn("slot lag metrics error", zap.Error(res.Err))
			return math.NaN()
		}
		db.connInfo.Scan(pgtype.NumericOID, binT, res.Rows[0][0], &lag)
		// db.logger.Info("slot lag", zap.Float64("lag", lag))

		return lag
	})

	prometheus.MustRegister(metricSlotLag)
}
