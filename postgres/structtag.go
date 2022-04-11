package postgres

import (
	"github.com/pg2es/search-replica/conftags"
	"github.com/pkg/errors"
)

func (t *Table) parseStructTag(tag string) error {
	if t.tagParsed {
		return nil
	}
	t.tagParsed = true

	tags, err := conftags.Parse(tag)
	if err != nil {
		return err
	}

	t.parseIndexTag(tags)
	t.parseInlineTags(tags)
	t.parseJoinTag(tags)
	return nil
}

func (t *Table) parseJoinTag(tags conftags.Tags) error {
	tag := tags.Get("join") // only one join is allowed per document.
	if tag == nil {         // however multiple relations allowed per index.
		return nil
	}

	t.join = tableJoin{
		enabled:   true,
		fieldName: "join",
		typeName:  t.docType,
	}

	if tag.Values[0] != "" {
		t.join.fieldName = tag.Values[0]
	}
	if len(tag.Values) > 1 {
		t.join.typeName = tag.Values[1]
	}

	return nil
}

func (t *Table) parseInlineTags(tags conftags.Tags) error {
	for _, tag := range tags.Filter("inline") {
		inline := t.schema.inline(tag.Values[0])

		// cross links
		inline.parent = t
		t.inlines = append(t.inlines, inline)

		// first option is field name
		if len(tag.Values) > 1 {
			inline.fieldName = tag.Values[1]
		}

		// second and third are ES script names. In case if we want custom logic there.
		if len(tag.Values) > 3 {
			inline.scriptAddID = tag.Values[2]
			inline.scriptDelID = tag.Values[3]
		}
	}

	return nil
}

func (t *Table) parseIndexTag(tags conftags.Tags) error {
	tag := tags.Get("index")
	if tag == nil {
		return nil // Tag does not exists. Nothing to change
	}

	// `index:"-"` means skip this table
	if tag.Values[0] == "-" {
		t.index = false
		// t.docType = ""
		return nil
	}

	if tag.Values[0] != "" {
		t.docType = tag.Values[0]
	}

	for _, opt := range tag.Values[1:] {
		switch opt {
		case "all":
			t.indexAll = true
		}
	}
	return nil
}

func (c *Column) parseStructTag(tag string) error {
	tags, err := conftags.Parse(tag)
	if err != nil {
		return errors.Wrapf(err, "parse column %s struct tag", c.name)
	}

	c.parseIndexTag(tags)
	c.parseInlineTags(tags)
	c.parseJoinTag(tags)
	return nil
}

func (col *Column) parseInlineTags(tags conftags.Tags) error {
	for _, tag := range tags.Filter("inline") {
		inline := col.table.schema.inline(tag.Values[0])

		// cross links
		if inline.source == nil {
			inline.source = col.table
			col.table.isInlinedIn = append(col.table.isInlinedIn, inline) // Add table to inlined in
		}

		if inline.source != col.table {
			return errors.New("only one table can be inline source")
		}

		name := col.name
		for _, opt := range tag.Values[1:] {
			switch opt {
			case "pk":
				inline.pkCol = col
			case "parent":
				inline.parentCol = col
			case "routing":
				inline.routingCol = col
			default: // field name
				name = opt
				// log.Print("renaming fields is not suported yet.")
			}
		}
		// XXX: Here we can detect 1:1 mapping, and use different inject script. Like following
		// if inline.pkCol == inline.parentCol {
		// inline.scriptAddID = "injectone"
		// inline.scriptAddID = "ejectone"
		// }
		// TBD: actual "painless" scripts

		inline.columns[name] = col
	}

	return nil
}

func (col *Column) parseIndexTag(tags conftags.Tags) error {
	tag := tags.Get("index")
	if tag == nil {
		return nil // Tag does not exists. Nothing to change
	}
	col.index = true

	if tag.Values[0] != "" {
		col.fieldName = tag.Values[0]
	}

	for _, opt := range tag.Values[1:] {
		switch opt {
		case "id":
			col.table.pkNoPrefix = true
			fallthrough
		case "pk":
			col.table.pkCol = col
		case "routing":
			col.table.routingCol = col
		}
	}

	if tag.Values[0] == "-" { // `index:"-"` means skip this column
		col.index = false
		col.fieldName = ""
	}

	return nil
}

func (col *Column) parseJoinTag(tags conftags.Tags) error {
	tag := tags.Get("join")
	if tag == nil {
		return nil // Tag does not exists. Nothing to change
	}

	col.table.join.enabled = true
	switch tag.Values[0] {
	case "name":
		col.table.join.nameCol = col
	case "parent":
		col.table.join.parentCol = col
	}

	return nil
}
