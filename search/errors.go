package search

import "encoding/json"

type BulkResponseErrors struct {
	Errors []BulkRowError
}

func (bs *BulkResponseErrors) UnmarshalJSON(b []byte) error {
	bs.Errors = bs.Errors[:0]
	tmp := struct {
		Errors bool `json:"errors,omitempty"`
		Items  []map[string]*struct {
			ID string `json:"_id,omitempty"`
			// Index string `json:"_index,omitempty"`
			// HTTPStatus int           `json:"status,omitempty"`
			Error *BulkRowError `json:"error,omitempty"`

			// concurrency?
			// Version int    `json:"_version"`
			// SeqNo         int64         `json:"_seq_no"`
			// PrimaryTerm   int64         `json:"_primary_term"`
		} `json:"items,omitempty"`
	}{}

	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	if !tmp.Errors {
		return nil
	}

Items:
	for _, mapWrapper := range tmp.Items {
		for _, row := range mapWrapper {
			if row == nil {
				continue Items
			}
			if row.Error == nil {
				continue Items
			}
			row.Error.DocID = row.ID
			bs.Errors = append(bs.Errors, *row.Error)
		}
	}
	return nil
}

type BulkRowError struct {
	DocID  string
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

func (err BulkRowError) Error() string {
	return err.Type + ": " + err.Reason
}
