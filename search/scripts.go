package search

import (
	_ "embed"
	"fmt"
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
		return fmt.Errorf("prepare inline_add: %w", err)
	}
	if err := e.client.Script("inline_del", inlineDelScript); err != nil {
		return fmt.Errorf("prepare inline_del: %w", err)
	}

	// Lucene does not support map fields. And there is no way to flaten them.
	return nil
}
