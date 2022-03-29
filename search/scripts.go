package search

import (
	_ "embed"

	"github.com/pkg/errors"
)

var (
	//go:embed scripts/inline_add.painless
	inlineAddScript string

	//go:embed scripts/inline_del.painless
	inlineDelScript string

	// might use: // go:embed scripts/inline_add_map.painless
	// inlineAddMapScript string
)

func (e *BulkElastic) PrepareScripts() error {
	if err := e.client.Script("inline_add", inlineAddScript); err != nil {
		return errors.Wrap(err, "prepare inline_add")
	}
	if err := e.client.Script("inline_del", inlineDelScript); err != nil {
		return errors.Wrap(err, "prepare inline_del")
	}

	// Lucene does not support map fields. And there is no way to flaten them.
	return nil
}
