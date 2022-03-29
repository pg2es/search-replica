package postgres

import (
	"context"
	"fmt"

	_ "embed"

	"github.com/jackc/pgtype"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const ( // shortcuts
	binT  = pgtype.BinaryFormatCode
	textT = pgtype.TextFormatCode
)

// TODO: check `wal_status` from `pg_replication_slots`. See: https://www.postgresql.org/docs/13/view-pg-replication-slots.html
// And if status is `lost`, start reindexing from scratch.

// discoverQuery selects all table and column comments for tables mentioned in publication.
// comments are used as golang structtags for configuration.
// TODO (vitalii): check if FK -> tablename can help with inlines
//go:embed queries/discover_query.sql
var discoverQuery string

// Discover uses postgress publication and table or column comments to generate replication config
// Only tables explosed via Publication will be considered for exporting to ES.
func (db *Database) Discover(ctx context.Context) error {
	db.queryConnMu.Lock()
	res := db.queryConn.ExecParams(
		ctx, discoverQuery,
		[][]byte{[]byte(db.Publication)}, nil, nil,
		[]int16{binT, binT, binT, binT, binT, binT, binT, binT},
	).Read()
	db.queryConnMu.Unlock()

	if res.Err != nil {
		return errors.Wrap(res.Err, "discover db tags config")
	}

	cd := struct {
		PK            pgtype.Bool
		Schema        pgtype.Text
		Table         pgtype.Text
		TableComment  pgtype.Text
		Column        pgtype.Text
		ColumnComment pgtype.Text
		Typ           pgtype.OID
		OldInWAL      pgtype.Bool
	}{}

	for _, row := range res.Rows {
		cd.Schema.DecodeBinary(nil, row[0])
		cd.Table.DecodeBinary(nil, row[1])
		cd.Column.DecodeBinary(nil, row[2])
		cd.TableComment.DecodeBinary(nil, row[3])
		cd.ColumnComment.DecodeBinary(nil, row[4])
		cd.PK.DecodeBinary(nil, row[5])
		cd.Typ.DecodeBinary(nil, row[6])
		cd.OldInWAL.DecodeBinary(nil, row[7])

		t := db.Schema(cd.Schema.String).Table(cd.Table.String)
		t.parseStructTag(cd.TableComment.String) // table config needs to be parsed before column config, since some values are inherited from it
		col := t.Column(cd.Column.String)
		col.parseStructTag(cd.ColumnComment.String)
		col.sqlPK = cd.PK.Bool
		col.oldInWAL = cd.OldInWAL.Bool

		dataType, err := db.DataType(uint32(cd.Typ))
		if err != nil {
			t.logger.Warn("can not find data type for column", zap.String("column", cd.Column.String))
		}
		col.setTyp(dataType)
	}
	return nil
}

// discoverENUMQuery can be used to retreive enum members, which is not required. Bug gives slightly increased performance.
//go:embed queries/discover_enum_query.sql
var discoverENUMQuery string

// discoverUnknownType fetches type description from postgres and saves into connInfo.
// works with Enum, EnumArray and Composite fields.
func (db *Database) discoverUnknownType(ctx context.Context, oid pgtype.OID) error {
	if _, ok := db.connInfo.DataTypeForOID(uint32(oid)); ok {
		return nil
	}

	typInfo, err := db.getTypInfo(ctx, oid)
	if err != nil {
		return errors.Wrap(err, "get type info")
	}

	switch typInfo.Typ.Int {
	case 'c':
		// Here are some examples of, how composite type casting is done in python psycopg
		// https://github.com/psycopg/psycopg2/blob/1d3a89a0bba621dc1cc9b32db6d241bd2da85ad1/lib/extras.py#L1089-L1097
		attrelid, err := typInfo.TypRelOID.EncodeBinary(db.connInfo, nil)
		if err != nil {
			return errors.Wrap(err, "can not encode OID parameter")
		}

		db.queryConnMu.Lock()
		res := db.queryConn.ExecParams(ctx,
			"select attname, atttypid from pg_attribute where attrelid=$1 order by attnum",
			[][]byte{attrelid}, []uint32{uint32(pgtype.OIDOID)}, []int16{binT},
			[]int16{binT, binT},
		).Read()
		db.queryConnMu.Unlock()

		if res.Err != nil {
			return res.Err
		}

		fields := make([]pgtype.CompositeTypeField, len(res.Rows))
		for i, row := range res.Rows {
			db.connInfo.Scan(pgtype.TextOID, binT, row[0], &(fields[i]).Name)
			db.connInfo.Scan(pgtype.OIDOID, binT, row[1], &(fields[i]).OID)
			// double check, for nested enums or composite types
			db.discoverUnknownType(ctx, pgtype.OID(fields[i].OID))
		}

		typ, err := pgtype.NewCompositeType(typInfo.Name.String, fields, db.connInfo)
		if err != nil {
			return err
		}

		db.connInfo.RegisterDataType(pgtype.DataType{
			Value: typ,
			Name:  typInfo.Name.String,
			OID:   uint32(typInfo.OID),
		})
		db.logger.Info("registered composite type", zap.Uint32("OID", uint32(typInfo.OID)), zap.String("name", typInfo.Name.String))
		if typInfo.ArrOID > 0 {
			db.logger.Warn("array composites are not yet supported", zap.Uint32("OID", uint32(typInfo.OID)), zap.String("name", typInfo.Name.String))
			// db.connInfo.RegisterDataType(pgtype.DataType{
			// Value: &pgtype.CompositeArray{}, // todo implement
			// Name:  typInfo.Name.String + "_array",
			// OID:   uint32(typInfo.ArrOID),
			// })
		}
	case 'e':
		db.connInfo.RegisterDataType(pgtype.DataType{
			Value: pgtype.NewEnumType(typInfo.Name.String, nil),
			Name:  typInfo.Name.String,
			OID:   uint32(typInfo.OID),
		})
		db.logger.Info("registered enum type", zap.Uint32("OID", uint32(typInfo.OID)), zap.String("name", typInfo.Name.String))
		if typInfo.ArrOID > 0 {
			db.connInfo.RegisterDataType(pgtype.DataType{
				Value: &pgtype.EnumArray{},
				Name:  typInfo.Name.String + "_array",
				OID:   uint32(typInfo.ArrOID),
			})
			db.logger.Info("registered enum array", zap.Uint32("OID", uint32(typInfo.OID)), zap.String("name", typInfo.Name.String))
		}
		return nil
		// resolve enum type
	case 'b':
		break
	default:
		return errors.New("unknown type")
	}

	switch typInfo.Cat.Int {
	case 'A':
		if typInfo.ElemOID > 0 {
			return db.discoverUnknownType(ctx, typInfo.ElemOID)
		}
		// if typInfo.TypRelOID > 0 { // Not supported yet
		// return db.discoverUnknownType(ctx, typInfo.TypRelOID)
		// }
		//
		// db.connInfo.RegisterDataType(pgtype.DataType{
		// Value: &pgtype.EnumArray{},
		// Name:  typInfo.Name.String + "_array",
		// OID:   uint32(typInfo.OID),
		// })
		// case 'U':

	}
	return nil
}

type typInfoRow struct {
	OID       pgtype.OID
	ArrOID    pgtype.OID
	ElemOID   pgtype.OID
	TypRelOID pgtype.OID // kinda relation, but for composite field types
	Name      pgtype.Text
	Typ       pgtype.QChar
	Cat       pgtype.QChar
}

//go:embed queries/get_type_by_oid.sql
var queryTypeByOID string

func (db *Database) getTypInfo(ctx context.Context, oid pgtype.OID) (*typInfoRow, error) {
	typOIDArg, err := oid.EncodeBinary(db.connInfo, nil)
	if err != nil {
		return nil, errors.Wrap(err, "can not encode OID parameter")
	}

	db.queryConnMu.Lock()
	res := db.queryConn.ExecParams(
		ctx, queryTypeByOID,
		[][]byte{typOIDArg}, []uint32{uint32(pgtype.OIDOID)}, []int16{binT},
		[]int16{binT, binT, binT, binT, binT, binT, binT},
	).Read()
	db.queryConnMu.Unlock()

	typInfo := typInfoRow{}

	if res.Err != nil {
		return nil, errors.Wrapf(res.Err, "type not found %v", oid)
	}
	if len(res.Rows) != 1 {
		return nil, fmt.Errorf("type request returned %v rows, 1 expected", len(res.Rows))
	}

	row := res.Rows[0]
	errs := make([]error, 7)
	errs[0] = typInfo.OID.DecodeBinary(nil, row[0])
	errs[1] = typInfo.Name.DecodeBinary(nil, row[1])
	errs[2] = typInfo.Typ.DecodeBinary(nil, row[2])
	errs[3] = typInfo.Cat.DecodeBinary(nil, row[3])
	errs[4] = typInfo.ArrOID.DecodeBinary(nil, row[4])
	errs[5] = typInfo.ElemOID.DecodeBinary(nil, row[5])
	errs[6] = typInfo.TypRelOID.DecodeBinary(nil, row[6])
	for _, err := range errs {
		if err != nil {
			return nil, errors.Wrap(err, "decode type response error")
		}
	}

	return &typInfo, nil
}
