package search

import (
	"bytes"
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/pg2es/search-replica/postgres"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// TODO: https://github.com/elastic/go-elasticsearch/blob/master/_examples/bulk/indexer.go

type BulkElastic struct {
	BufferSize int // in bytes
	PushPeriod time.Duration

	Config *postgres.Database

	client *Client

	wg     sync.WaitGroup
	mu     sync.Mutex
	buf    *bytes.Buffer
	ticker *time.Ticker

	// cond sync.Cond

	inqueue pglogrepl.LSN

	logger *zap.Logger
}

func (e *BulkElastic) Logger(logger *zap.Logger) {
	e.logger = logger
}

func (e *BulkElastic) Connect(ctx context.Context, host, username, password string) (err error) {
	// TODO: use ctx for a timeout, for initial PING, etc
	e.client, err = NewClient(host, username, password)
	return errors.Wrap(err, "connecting to Search")
}

func (e *BulkElastic) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	buf := make([]byte, 0, e.BufferSize)
	e.buf = bytes.NewBuffer(buf)
	e.ticker = time.NewTicker(e.PushPeriod)

	// testing cond
	// e.cond = *sync.NewCond(&sync.Mutex{})

	go func() {
		errCount := 0
		for {
			select {
			case <-e.ticker.C:
				if err := e.Exec(); err != nil {
					log.Print(err)
					errCount++
					if errCount >= 10 {
						e.logger.Fatal("repeating errors", zap.Error(err))
					}
				} else {
					errCount = 0
				}

			}
		}
	}()

	// e.ticker = time.NewTicker(e.PushPeriod)
}

func (e *BulkElastic) resetTicker() {
	e.ticker.Reset(e.PushPeriod)
	select {
	case <-e.ticker.C:
	default:
	}
}

func (e *BulkElastic) SetCfg(cfg *postgres.Database) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Config = cfg
}

func (e *BulkElastic) Add(pos pglogrepl.LSN, buffers ...[]byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// TODO: update LSN to latest server position,
	// even without operations on published tables
	if len(buffers) == 0 {
		e.inqueue = pos // Will update LSN after flushing current buffer
		return nil
	}

	var size int
	for _, b := range buffers {
		size += len(b) + 1 // +1 for newline
	}

	// buffer is full, commit query
	if e.buf.Cap()-e.buf.Len() < size {
		// <-e.ticker.C // lock untill next batch time. // XXX: conflicts with Start goroutine
		if err := e.exec(); err != nil {
			log.Print(err)
			return err
		}
	}
	// TODO: commit by timeout; every 30s for ex

	for _, b := range buffers {
		e.buf.Write(b)
		e.buf.WriteByte('\n')
	}
	e.inqueue = pos

	return nil
}

func (e *BulkElastic) Exec() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exec()
}

func (e *BulkElastic) exec() error {
	// Bulk request without data will return 400 error. So, in case if there was no writes and LSN cursor hasn't changed, we skip this.
	// However, during cold start, wee need to push a lot of data, without LSN positions.
	// TODO: Improve bulk handling and buffers
	committed := e.Config.LSN()
	if e.buf.Len() == 0 || (e.inqueue != pglogrepl.LSN(0) && committed == e.inqueue) {
		e.Config.CommitLSN(e.inqueue)
		return nil // nothing to push
	}

	defer e.resetTicker()

	// Wrapped into separate reader to make retry possible.
	// Since buffer would read from last position, even in case of error
	body := bytes.NewReader(e.buf.Bytes())

	size := e.buf.Len()
	if err := e.client.Bulk(body); err != nil {
		return errors.Wrap(err, "commit bulk request")
	}

	e.Config.CommitLSN(e.inqueue)
	e.buf.Reset()
	e.logger.Info("pushed pulk requestd", zap.Int("size", size), zap.String("LSN", e.inqueue.String()))
	return nil
}

type RespItemErr struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	IndexUUID string `json:"index_uuid"` // how to decode "aAsFqTI0Tc2W0LCWgPNrOA"?
	Shard     string `json:"shard"`      //  int as a string
	Index     string `json:"index"`
}

func (e RespItemErr) Error() string {
	return "[" + e.Index + "]" + e.Reason
}
