package postgres

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/jackc/pglogrepl"
)

var (
	ErrTimeout  = errors.New("timeout")
	ErrChClosed = errors.New("document channel is closed")
)

// StreamPipe connects decoded postgres stream with elasticsearch client. There should not be any other interaction.
type StreamPipe struct {
	ch  chan Doc
	pos pglogrepl.LSN
	ctx context.Context
}

func NewStreamPipe(ctx context.Context) *StreamPipe {
	return &StreamPipe{
		ctx: ctx,
		ch:  make(chan Doc), // buffered?
	}
}

func (p *StreamPipe) add(d Doc) {
	select {
	case p.ch <- d:
	case <-p.ctx.Done():
		//drain documents without adding
	}
}

func (p *StreamPipe) Position() pglogrepl.LSN {
	return pglogrepl.LSN(atomic.LoadUint64((*uint64)(&p.pos)))
}

func (p *StreamPipe) Next(ctx context.Context) (Doc, error) {
	select {
	case doc, ok := <-p.ch:
		if !ok {
			return nil, ErrChClosed
		}
		return doc, nil
	case <-ctx.Done():
		return nil, ErrTimeout
	case <-p.ctx.Done():
		// shutting down
		return nil, ErrTimeout
	}

}

func (p *StreamPipe) CommitPosition(pos pglogrepl.LSN) {
	atomic.StoreUint64((*uint64)(&p.pos), uint64(pos))
}

type Position pglogrepl.LSN

func (p Position) LSN() pglogrepl.LSN {
	return pglogrepl.LSN(p)
}

func (d Position) NDJSON() [][]byte {
	return nil
}

// Document represents one single operation in bulk request.
type Document struct {
	Position
	Meta []byte // Op type, index and document id
	Data []byte // document content or script
}

func (d Document) NDJSON() [][]byte {
	if d.Data != nil {
		return [][]byte{d.Meta, d.Data}
	}
	return [][]byte{d.Meta}
}

type Doc interface {
	NDJSON() [][]byte
	LSN() pglogrepl.LSN
}
