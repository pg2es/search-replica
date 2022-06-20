// package pgcopy - parser for PostgreSQL `COPY TO ... WITH BINARY` command result
// FileFormat: https://www.postgresql.org/docs/current/sql-copy.html#id-1.9.3.55.9.4.5
package pgcopy

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"

	"github.com/pkg/errors"
)

var (
	ErrInvalidSignature = errors.New(`invalid file signature: expected PGCOPY\n\377\r\n\0`)
)

const signature = "PGCOPY\n\377\r\n\x00" // \0 is replaced with \x00, due to Golang syntax

type Parser struct {
	ch chan [][]byte
}

func NewParser() *Parser {
	return &Parser{ch: make(chan [][]byte)}
}

func (p *Parser) Next(ctx context.Context) ([][]byte, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case row, ok := <-p.ch:
			if !ok {
				return nil, io.EOF
			}
			return row, nil
		}
	}
}

// TODO: move to pgio
func readInt32(r io.Reader) int32 {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0
	}
	return int32(binary.BigEndian.Uint32(buf[:]))
}
func readInt16(r io.Reader) int16 {
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0
	}
	return int16(binary.BigEndian.Uint16(buf[:]))
}

func (p *Parser) Parse(r io.Reader) error {
	sign := make([]byte, len(signature))
	if _, err := io.ReadFull(r, sign); err != nil {
		return ErrInvalidSignature
	}
	if !bytes.Equal(sign, []byte(signature)) {
		return ErrInvalidSignature
	}
	flags := readInt32(r)
	extensionSize := readInt32(r)
	_ = flags
	extension := make([]byte, extensionSize)
	if _, err := io.ReadFull(r, extension); err != nil {
		return errors.Wrap(err, "can't read header extension")
	}

	for {
		// Presently, all tuples in a table will have the same count, but that might not always be true.
		tupleLen := readInt16(r)
		if tupleLen == -1 { // EOF
			break
		}
		row := make([][]byte, tupleLen)

		// Reading columns
		// TODO: optimize with buffers
		for i := 0; i < int(tupleLen); i++ {
			colLen := readInt32(r)
			if colLen == -1 {
				continue // column is nil
			}
			row[i] = make([]byte, colLen)
			if _, err := io.ReadFull(r, row[i]); err != nil {
				return errors.Wrap(err, "can't read column")
			}
		}
		p.ch <- row
	}

	close(p.ch)
	return nil
}
