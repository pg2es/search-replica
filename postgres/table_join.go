package postgres

import "encoding/json"

// https://www.elastic.co/guide/en/elasticsearch/reference/current/parent-join.html
type tableJoin struct {
	enabled   bool
	fieldName string // where in _source it should be stored
	typeName  string // hardcoded type

	nameCol   *Column // dynamic type from column value; polymorphysm
	parentCol *Column // value of parent id
}

func (tj *tableJoin) jsonKey() string {
	return tj.fieldName
}

func (tj *tableJoin) jsonFromRow(row [][]byte) ([]byte, error) {
	joinObj := documentJoin{
		Name: tj.typeName,
	}
	if tj.nameCol != nil { // dynamic type from value
		joinObj.Name = tj.nameCol.stringFromRow(row)
	}

	// for child documents
	if tj.parentCol != nil { // this doc is a child.
		joinObj.Parent = tj.parentCol.stringFromRow(row)
	}

	return json.Marshal(joinObj)
}
