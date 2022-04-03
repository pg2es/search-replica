// package main ...
//   _______  _______    _       _______  _______
//  (  ____ )(  ____ \  ( \     (  ____ \(  ____ \
//  | (    )|| (    \/   \ \    | (    \/| (    \/
//  | (____)|| |          \ \   | (__    | (_____
//  |  _____)| | ____      ) )  |  __)   (_____  )
//  | (      | | \_  )    / /   | (            ) |
//  | )      | (___) |   / /    | (____/\/\____) |
//  |/       (_______)  (_/     (_______/\_______)
//
package main

import (
	"context"
	"flag"
	"log"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/pg2es/search-replica/postgres"
	"github.com/pg2es/search-replica/search"
)

const OutputPlugin = "pgoutput" // important

var Version = "master"

var (
	pgSlotCreate   bool
	pgSlotReCreate bool
	reindex        bool
)

func init() {
	flag.BoolVar(&pgSlotCreate, "create", false, "Create new replication slot, if specified slot does not exists.")
	flag.BoolVar(&pgSlotReCreate, "recreate", false, "Deletes slot and creates new one.")
	flag.BoolVar(&reindex, "reindex", false, "Start with a backup to populate data into empty ES cluster (not implemented)")
}

func initLogger(format, level string) (logger *zap.Logger, err error) {

	cfg := zap.NewProductionConfig() // default
	if format == "cli" {
		cfg = zap.NewDevelopmentConfig()
	}

	// cfg.DisableCaller = true // disable file:line
	cfg.DisableStacktrace = true
	cfg.Level, err = zap.ParseAtomicLevel(level)
	if err != nil {
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}
	return cfg.Build()
}

func main() {
	var err error

	defer log.Print("Our K8S in Cluster,...\nForgive us our error logs\ngive us our daily RAM\nrestart us in case of failure\n")
	cfg := FromEnv()
	flag.Parse()

	logger, _ := initLogger(cfg.LogFormat, cfg.LogLevel)
	defer logger.Sync()
	logger.Info("Starting the SearchReplica", zap.String("version", Version))

	ctx := context.Background()
	// TODO: waitgroups and gracefull shutdown

	searchClient := &search.BulkElastic{
		BufferSize: 1024 * 1024 * cfg.Search.BulkSizeLimit, // Good one would be 4-8mb; limit ~100MB
		PushPeriod: cfg.Search.PushInterval,
	}
	searchClient.Logger(logger)
	if err := searchClient.Connect(ctx, cfg.Search.URL, cfg.Search.User, cfg.Search.Password); err != nil {
		logger.Fatal(err.Error())
	}
	if err := searchClient.PrepareScripts(); err != nil {
		logger.Fatal(err.Error())
	}
	searchClient.Start()

	db := postgres.New(logger)
	db.SlotName = cfg.Postgres.Slot
	db.Publication = cfg.Postgres.Publication
	if err := db.Connect(ctx); err != nil { // This will implicitly use PG* env variables
		logger.Fatal(errors.Wrap(err, "connect to DB").Error())
	}
	defer db.Close(ctx)

	if err := db.Discover(ctx); err != nil {
		logger.Fatal(errors.Wrap(err, "discover config").Error())
	}

	searchClient.SetCfg(db)
	db.PrintSatus()

	var msgCounter uint64
	var msgCounterAll uint64
	go func() {
		for {
			msg, err := db.NextMessage(ctx)
			if err != nil {
				logger.Error("Recv message error", zap.Error(err))
				break
			}
			if len(msg.Meta) > 0 { // ignore LSN standby updates
				atomic.AddUint64(&msgCounter, 1)
				atomic.AddUint64(&msgCounterAll, 1)
			}

			if len(msg.Data) > 0 {
				searchClient.Add(msg.LSN, msg.Meta, msg.Data)
				continue
			}
			if len(msg.Meta) > 0 { // E.G: delete
				searchClient.Add(msg.LSN, msg.Meta)
				continue
			}
			searchClient.Add(msg.LSN)
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			count := atomic.SwapUint64(&msgCounter, 0)
			countAll := atomic.LoadUint64(&msgCounterAll)
			if count > 0 {
				logger.Info("forwarded messages", zap.Uint64("last", count), zap.Uint64("all", countAll))
			}
		}
	}()

	if pgSlotReCreate {
		db.DropReplicationSlot(ctx)
		pgSlotCreate = true
	}
	// During slot creation, Postgres also make a spanshot of a database. Freezeng a state for the following COPY command  or backup. Snapshot is available within this transaction.
	if err := db.Tx(ctx); err != nil {
		logger.Fatal("start transaction", zap.Error(err))
	}
	if pgSlotCreate {
		db.CreateReplicationSlot(ctx)
	}

	startAt := pglogrepl.LSN(0) //Zero value means: Get last commited position for this slot from master
	if reindex {
		logger.Info("REINDEX")
		err := db.Reindex(ctx) // blocking; should be called in same transaction as slot creation
		if err != nil {
			log.Fatal(err)
		}
	}

	// Since we do not need snapshot anymore, we can commit the transaction.
	// And we need to do so, because streaming replication is not possible within a transaction.
	if err := db.Commit(ctx); err != nil {
		logger.Fatal("commit transaction", zap.Error(err))
	}

	// TODO: move to metrics
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for _ = range ticker.C {
			if err := db.Lag(ctx); err != nil {
				logger.Error("check lag", zap.Error(err)) // TODO: periodiacl check
			}
		}
	}()

	err = db.StartReplication(ctx, startAt) // blocking
	if err != nil {
		logger.Fatal("replication error", zap.Error(err))
	}

}
