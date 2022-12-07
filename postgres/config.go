package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgtype"
	"go.uber.org/zap"
)

var (
	// ErrZeroType Postgres type OID can not be zero
	ErrZeroType = errors.New("postgres type OID can't be zero")
	// ErrTypeNotFound can not find type OID among discovered types
	ErrTypeNotFound = errors.New("can not find discovered type")
)

func New(stream *StreamPipe, logger *zap.Logger) *Database {
	return &Database{
		schemas:        make(map[string]*Schema),
		relationSet:    make(map[uint32]*Table),
		StandbyTimeout: 10 * time.Second,
		logger:         logger,
		stream:         stream,
		connInfo:       pgtype.NewConnInfo(),
	}
}

type Database struct {
	name        string
	schemas     map[string]*Schema
	relationSet map[uint32]*Table // index cache, by Postgres Relation OID

	replConn    *pgconn.PgConn // streaming replication
	queryConn   *pgconn.PgConn // config and types discovery
	queryConnMu sync.Mutex     // pgconn.PgConn is not thread safe.

	connInfo       *pgtype.ConnInfo
	version        string
	SlotName       string
	useBinary      bool
	Publication    string
	StandbyTimeout time.Duration

	stream *StreamPipe
	logger *zap.Logger
}

// indexableTables returns filtered list of tables, that's are subject to be indexed
// helper function
func (db *Database) indexableTables() (tables []*Table) {
	for _, sc := range db.schemas {
		for _, tc := range sc.tables {
			if tc.index || len(tc.isInlinedIn) > 0 {
				tables = append(tables, tc)
			}
		}
	}

	// Sort tables by inlines, so document is created first, and inlined fields added after.
	// parent first, then sources
	if len(tables) < 2 {
		return tables
	}

	pos := make(map[*Table]int, len(tables))
	for i, prt := range tables {
		pos[prt] = i

		j := i
		for _, inl := range prt.inlines {
			if loc, ok := pos[inl.source]; loc < j && ok {
				j = loc
			}
		}
		if j != i { // swap parent with first source
			tables[i], tables[j] = tables[j], tables[i]
		}
	}

	return tables
}

// dataTypDecoder returns DecoderValue for specific type by OID.
// for user defined types it tries to resolve basic type and returns DecoderValue for it.
// Does not support arrays of composite types, however arrays and composite types are supported.
// For changes in decoding or json marhasling, check pgtype package itself.
func (db *Database) dataTypeDecoder(oid uint32) (*pgtype.DataType, error) {
	if oid == 0 {
		return nil, ErrZeroType
	}
	if err := db.discoverUnknownType(context.Background(), pgtype.OID(oid)); err != nil {
		return nil, fmt.Errorf("can not discover type: %w", err)
	}

	typ, ok := db.connInfo.DataTypeForOID(oid)
	if !ok {
		return nil, ErrTypeNotFound
	}

	return typ, nil

}

// returns table (relation) by OID or nil if not found
func (db *Database) relation(oid uint32) (tc *Table) {
	if r, ok := db.relationSet[oid]; ok {
		return r
	}
	// TODO: remove fallback search
	for _, schema := range db.schemas {
		for _, table := range schema.tables {
			if table.relID == oid {
				db.relationSet[oid] = table
				return table
			}
		}
	}
	return nil
}

// schema returns (and creates if required) initialized schema config
func (db *Database) schema(name string) (sc *Schema) {
	if _, exists := db.schemas[name]; !exists {
		db.schemas[name] = &Schema{
			name:      name,
			database:  db,
			tables:    make(map[string]*Table),
			inlines:   make(map[string]*Inline),
			enumTypes: make(map[string]*pgtype.EnumType),
		}
	}

	return db.schemas[name]
}

// Schema describes Postgres schema/namespace
type Schema struct {
	name string
	// Add ES index col
	tables   map[string]*Table
	database *Database

	inlines map[string]*Inline // All inlines by name

	// custom enum types by name.
	// same types an be accessed on DB level by OID
	enumTypes map[string]*pgtype.EnumType
}

func (sc *Schema) table(name string) (tc *Table) {
	if _, exists := sc.tables[name]; !exists {
		sc.tables[name] = &Table{
			schema:  sc,
			name:    name,
			docType: name, // default
			columns: make(map[string]*Column),
			index:   true, // index all by default. Since it's already specified in publication
			logger:  sc.database.logger.With(zap.String("table", name)),
		}
	}

	return sc.tables[name]
}

// inline returns new, or existing inline by name with reasonable defaults
func (sc *Schema) inline(name string) (inl *Inline) {
	if _, exists := sc.inlines[name]; !exists {
		sc.inlines[name] = &Inline{
			name:        name,
			fieldName:   name,
			columns:     make(map[string]*Column),
			scriptAddID: "inline_add",
			scriptDelID: "inline_del",
			logger:      sc.database.logger.With(zap.String("inline", name)),
		}
	}

	return sc.inlines[name]
}

// PrintStatus prints some debug information
// TODO: remove
func (db *Database) PrintSatus() {
	log.Print("* * * DB STATUS * * *")
	for _, schema := range db.schemas {
		log.Print(schema.name)
		for _, table := range schema.tables {
			table.init()
			log.Printf(" - %s\n", table.name)
			if table.upsertOnly {
				log.Print("   WARNING: Table is forwarded in upsert only mode. Not all key fields are awailable in WAL")
			}
			for _, column := range table.columns {
				cdesc := "   "
				if column.index {
					cdesc += "+ "
				} else {
					cdesc += "- "
				}
				cdesc += column.name
				if column.name != column.fieldName {
					cdesc += " -> " + column.fieldName
				}
				if column == column.table.pkCol { // same pointer
					cdesc += " PK"
				}
				if column == column.table.routingCol { // same pointer
					cdesc += " R"
				}
				if column.oldInWAL { // same pointer
					cdesc += " WAL"
				}
				log.Print(cdesc)
			}
			for _, inline := range table.inlines {
				log.Printf(
					"   @ %s.%s\t%s\t[%s.%s == %s.%s]\n",
					inline.parent.name, inline.fieldName,
					inline.name,
					inline.source.name, inline.parentCol.name,
					inline.parent.name, inline.parent.pkCol.name,
				)
			}
		}
		log.Print(" * * * INLINES * * *")
		for _, inline := range schema.inlines {
			log.Printf(" - %s \t%s.%s\n", inline.name, inline.parent.name, inline.fieldName)
			if inline.upsertOnly {
				log.Print("   WARNING: inline is forwarded in upsert only mode. Not all key fields are awailable in WAL")
			}
			for name, col := range inline.columns {
				log.Printf("   + %s (%s)", name, col.name)
			}
		}
	}
}
