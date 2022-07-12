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
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/pg2es/search-replica/postgres"
	"github.com/pg2es/search-replica/search"
)

var Version = "master"

func initLogger(format, level string) (logger *zap.Logger, err error) {
	cfg := zap.NewProductionConfig() // default
	if format == "cli" {
		cfg = zap.NewDevelopmentConfig()
	}

	cfg.DisableCaller = true // disable file:line
	cfg.DisableStacktrace = true
	cfg.Level, err = zap.ParseAtomicLevel(level)
	if err != nil {
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}
	return cfg.Build()
}

func main() {
	var err error

	cfg := FromEnv()
	flag.Parse()

	logger, _ := initLogger(cfg.LogFormat, cfg.LogLevel)
	defer logger.Sync()
	logger.Info("Starting the SearchReplica", zap.String("version", Version))

	ctx, rootCancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	// TODO: waitgroups and gracefull shutdown

	stream := postgres.NewStreamPipe(ctx)

	searchClient, err := search.NewElastic(search.BulkElasticOpts{
		Host:         cfg.Search.URL,
		Username:     cfg.Search.User,
		Password:     cfg.Search.Password,
		BulkSize:     cfg.Search.BulkSizeLimit,
		IdleInterval: cfg.Search.PushInterval,
		Logger:       logger,
		Stream:       stream,
		Throttle:     cfg.Search.PushThrottle,
		Debounce:     cfg.Search.PushDebounce,
	})
	if err != nil {
		logger.Fatal(err.Error())
	}

	if err := searchClient.PrepareScripts(); err != nil {
		logger.Fatal(err.Error())
	}
	searchClient.Start(wg, ctx)

	db := postgres.New(stream, logger)
	db.SlotName = cfg.Postgres.Slot
	db.Publication = cfg.Postgres.Publication
	if err := db.Connect(ctx); err != nil { // This will implicitly use PG* env variables
		logger.Fatal(errors.Wrap(err, "connect to DB").Error())
	}
	defer db.Close(ctx)

	if err := db.Discover(ctx); err != nil {
		logger.Fatal(errors.Wrap(err, "discover config").Error())
	}

	db.RegisterSlotLagMetric(ctx)
	db.PrintSatus()

	mux := http.NewServeMux()
	mux.HandleFunc("/state", stateFunc)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
	mux.Handle("/metrics", promhttp.Handler())
	server := http.Server{
		Addr:    cfg.Address,
		Handler: mux,
	}

	//
	// API & Metrics
	//
	wg.Add(1)
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("API server closed with error", zap.Error(err))
		} // TODO: tls?
		wg.Done()
	}()

	startupDone := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer state.Store("started up")
		defer close(startupDone) // unlock streaming replication

		pgSlotReCreate = pgSlotReCreate || reindex
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

		if reindex {
			logger.Info("REINDEXING DATA")
			state.Store("reindexing")
			if err := db.Reindex(ctx); err != nil { // blocking; should be called in same transaction as slot creation
				logger.Fatal("reindexing failed", zap.Error(err))
			}
			state.Store("reindexing: done")
		}

		// After copying snapshot, which represents slot state, we should finish transaction before streaming replication
		if err := db.Commit(ctx); err != nil {
			logger.Fatal("commit transaction", zap.Error(err))
		}
	}()

	//
	// Replication
	//
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-startupDone // wait for subscription and initial reindexing
		// Zero value means: Get last commited position for this slot from master
		state.Store("streaming wal")
		if err = db.StartReplication(ctx, pglogrepl.LSN(0)); err != nil {
			logger.Fatal("replication error", zap.Error(err))
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch // lock here
	go func() {
		<-ch
		logger.Fatal("received second signal; Dying now!")
	}()
	state.Store("shutting down")
	logger.Info("shutting down gracefully")
	// TODO:
	// push to elastic
	// commit LSN to PG
	// die
	rootCancel() // canceling root context

	// shutdown
	wtimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	server.Shutdown(wtimeout)
	cancel()

	wg.Wait()

}
