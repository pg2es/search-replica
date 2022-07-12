package search

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/pg2es/search-replica/postgres"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var (
	metricMessageCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "search_doc_operations",
		Help: "number of doc index/update/deletes",
	})
	metricMessageSize = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "search_doc_size",
		Help: "Total size of JSON that was pushed to elastic",
	})
	// docs per request
	// request time
	// errors // retries
)

func init() {
	prometheus.MustRegister(metricMessageCount)
	prometheus.MustRegister(metricMessageSize)
}

type BulkElasticOpts struct {
	Host     string
	Username string
	Password string

	//
	Logger *zap.Logger
	// Maximum waiting time for data. Any partial bulk request will be pushed after idleInterval.
	// Default: 5s
	IdleInterval time.Duration
	// Minimal time between requests.
	// Default: 500ms
	Throttle time.Duration
	// Time after request, if
	Debounce time.Duration
	// Maximum bulk request
	// Good one would be 4-8mb; limit ~100MBsize in megabytes
	// Default: 4M
	BulkSize int
	// document stream
	Stream *postgres.StreamPipe
}

func NewElastic(opts BulkElasticOpts) (es *BulkElastic, err error) {
	if opts.IdleInterval < time.Second {
		opts.IdleInterval = 5 * time.Second
	}
	if opts.Throttle == 0 {
		opts.Throttle = 500 * time.Millisecond
	}
	if opts.Debounce == 0 {
		opts.Debounce = 100 * time.Millisecond
	}
	if opts.BulkSize == 0 {
		opts.BulkSize = 4
	}
	if opts.BulkSize > 100 {
		opts.BulkSize = 100
	}

	if opts.Stream == nil {
		return nil, errors.New("document stream can not be empty")
	}

	// allocate buffer of bulk size
	buf := make([]byte, 0, opts.BulkSize<<20)

	es = &BulkElastic{
		logger:        opts.Logger,
		stream:        opts.Stream,
		buf:           bytes.NewBuffer(buf),
		idle:          opts.IdleInterval,
		idleTimer:     time.NewTimer(opts.IdleInterval),
		throttle:      opts.Throttle,
		debounce:      opts.Debounce,
		throttleTimer: time.NewTimer(opts.Throttle),
		debounceTimer: time.NewTimer(opts.Debounce),
		// lastReqAt     :time.Now().
		cond: sync.NewCond(&sync.Mutex{}),
	}

	if es.client, err = NewClient(opts.Host, opts.Username, opts.Password, opts.Logger); err != nil {
		return nil, err
	}

	return es, nil

}

type BulkElastic struct {
	stream *postgres.StreamPipe
	client *Client
	wg     sync.WaitGroup

	// sync; state of buffer and timeouts
	idle           time.Duration
	idleTimer      *time.Timer
	throttle       time.Duration
	debounce       time.Duration
	throttleTimer  *time.Timer
	debounceTimer  *time.Timer
	lastReqAt      time.Time
	cond           *sync.Cond
	buf            *bytes.Buffer
	full           bool
	debounceStatus debounceStatus
	shutdown       bool

	inqueue pglogrepl.LSN

	logger *zap.Logger
}

type debounceStatus uint8

const (
	debounceSkip debounceStatus = iota
	debounceRequest
	debounceWait
)

func (e *BulkElastic) Start(wg *sync.WaitGroup, ctx context.Context) {

	wg.Add(1)
	go func() { //read loop
		defer wg.Done()
		for {
			msg, err := e.stream.Next(ctx) // no timeout. Graceful shutdown
			if err == postgres.ErrTimeout {
				select {
				case <-ctx.Done():
					e.logger.Info("shutdown: stoped accepting messages")
					return
				default:
					continue
				}
			}

			if err != nil { // postgres.ErrTimeout
				e.logger.Error("Recv message error", zap.Error(err))
				return
			}
			if doc, ok := msg.(postgres.Document); ok {
				metricMessageCount.Inc()
				e.logger.Debug("document",
					zap.Any("meta", json.RawMessage(doc.Meta)),
					zap.Any("data", json.RawMessage(doc.Data)),
				)
			}

			e.Add(msg.LSN(), msg.NDJSON()...)
		}
	}()

	wg.Add(1)
	go func() { // PUSH / EXEC
		defer wg.Done()
		errCount := 0
		// retry := false

		for {

			e.cond.L.Lock()
			// condition check loop. Label is used for additional verbosity
		ConditionCheck:
			for { // "condition check" loop lock

				// app is shutting down. Push existing buffer now.
				if e.shutdown {
					break ConditionCheck
				}

				// Request throttle. Wait and restart condition check.
				if !e.lastReqAt.Add(e.throttle).Before(time.Now()) {
					e.cond.Wait()
					continue ConditionCheck
				}

				// push full document
				if e.full {
					break ConditionCheck
				}

				// wait for idle push timeout and restart condition check.
				if !e.lastReqAt.Add(e.idle).Before(time.Now()) {
					e.cond.Wait()
					continue ConditionCheck
				}

				// timer is our, but nothing to push -> request debounce on next request
				if e.buf.Len() == 0 {
					e.debounceStatus = debounceRequest
					e.cond.Wait()
					continue ConditionCheck
				}

				// if debounce wait was requested, set timeout and wait
				if e.debounceStatus == debounceRequest { // buf is not empty
					e.debounceTimer.Reset(e.debounce)
					e.debounceStatus = debounceWait
					e.cond.Wait()
					continue ConditionCheck
				}
				if e.debounceStatus == debounceWait {
					e.cond.Wait()
					continue ConditionCheck
				}

				// push partial bulk request
				break ConditionCheck
			}
			// action
			for err := e.exec(); err != nil; errCount++ {
				e.logger.Warn("retrying", zap.Int("attempt", errCount+1), zap.Error(err))
				// TODO: allow adding documents to buffer between retries.
				time.Sleep(e.throttle)
				if errCount >= 2 { // after 3 errors
					e.logger.Fatal("repeating errors", zap.Int("attempt", errCount+1), zap.Error(err))
				}
			}

			e.lastReqAt = time.Now()
			e.throttleTimer.Reset(e.throttle)
			e.idleTimer.Reset(e.idle)
			e.full = false
			e.debounceStatus = debounceSkip

			if e.shutdown {
				e.cond.L.Unlock()
				return
			}
			e.cond.L.Unlock()
		}
	}()

	wg.Add(1)
	go func() { // "notify" ticker loop
		defer wg.Done()
		for {
			select {
			case <-e.idleTimer.C: // allows partial request
				e.cond.Broadcast()
			case <-e.debounceTimer.C: // trigger bulk, after short wait
				e.debounceStatus = debounceSkip
				e.cond.Broadcast()
			case <-e.throttleTimer.C: // allows full buffer request
				e.cond.Broadcast()
			case <-ctx.Done(): // shutdown
				e.cond.L.Lock()
				e.shutdown = true
				e.cond.Broadcast()
				e.cond.L.Unlock()
				return
			}
		}
	}()

}

func (e BulkElastic) push(ctx context.Context) {
	e.cond.L.Lock()
	for {

		if e.buf.Len() > 0 {

		}
	}

	e.cond.L.Unlock()
}

func (e *BulkElastic) Add(pos pglogrepl.LSN, buffers ...[]byte) error {
	e.cond.L.Lock()
	defer e.cond.L.Unlock()
	// TODO: update LSN to latest server position,
	// even without operations on published tables

	// Fast update LSN position
	if len(buffers) == 0 {
		e.inqueue = pos // Will update LSN after flushing current buffer
		if e.buf.Len() == 0 {
			e.stream.CommitPosition(pos)
		}
		return nil
	}

	var size int
	for _, b := range buffers {
		size += len(b) + 1 // +1 for newline
	}

	// buffer is full, wait for changes
	for e.buf.Cap()-e.buf.Len() < size {
		e.full = true
		e.cond.Broadcast() // unlock push and wait for it to finish
		e.cond.Wait()
	}
	e.full = false

	for _, b := range buffers {
		e.buf.Write(b)
		e.buf.WriteByte('\n')
	}
	e.inqueue = pos
	e.cond.Broadcast() // try to unlock push, in case if timers already expired

	return nil
}

// exec should be called with mutex locked.
func (e *BulkElastic) exec() error {
	size := e.buf.Len()
	if size == 0 {
		return nil // nothing to push; possible during shutdown
	}

	// Wrapped into separate reader to make retry possible.
	// Since buffer would read from last position, even in case of error
	body := bytes.NewReader(e.buf.Bytes())

	if err := e.client.Bulk(body); err != nil {
		return errors.Wrap(err, "commit bulk request")
	}

	if e.inqueue != pglogrepl.LSN(0) { // do not commit zero positions during reindexing
		e.stream.CommitPosition(e.inqueue)
	}
	metricMessageSize.Add(float64(size))
	e.buf.Reset()
	e.logger.Info("pushed bulk request", zap.Int("size", size), zap.String("LSN", e.inqueue.String()))
	return nil
}
