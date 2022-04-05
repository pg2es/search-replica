// package demo_test does basic consistency checks using the same CSV files from ./data subfolder.
// This is not a full test, but it's enough to check that data is injested properly.
// In order to use it, start with fresh setup:
// sh: docker-compose down -v && docker-compose up -d && sleep 10 && go test ./consistency_test.go
//
// It goes through all CSV files and checks that all documents are present in ES and values are correct.
// Using document fetch instead of search makes it quite fast.
// Data in CSV files MUST be sorted. You can generate bigger files witl `gen_csv.py [number]`
//
// This solution is temporary and will be replaced with proper testing later.
package demo_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

type ConsistencyTest struct {
	client  *searchClient
	csvData *csvData
}

func TestConsistency(t *testing.T) {
	is := require.New(t)

	f1, err1 := os.Open("data/main_doc.csv")
	is.NoError(err1)
	defer f1.Close()
	f2, err2 := os.Open("data/child_doc.csv")
	is.NoError(err2)
	defer f2.Close()
	f3, err3 := os.Open("data/inline_doc.csv")
	is.NoError(err3)
	defer f3.Close()

	data := newCSVData(f1, f2, f3)
	client := newClient("http://127.0.0.1:9200", "postgres", 1)
	var err error
	for err == nil {
		main, childs, inlines, err := data.Next()
		if err == io.EOF {
			break
		}
		is.NoError(err)
		_ = inlines
		_ = childs

		t.Run(main.id, func(t *testing.T) {
			is := require.New(t)

			maindoc := &mainDoc{}
			err := client.GetSource(main.id, main.id, maindoc)
			is.NoError(err)
			assertMain(is, main, maindoc)

			t.Run("inlined_field", func(t *testing.T) {
				is := require.New(t)

				var expected []inlineDoc
				for _, inline := range inlines {
					expected = append(expected, inlineDoc{ID: inline.id, ParentID: inline.parentID, Value: inline.value})
				}

				is.ElementsMatch(expected, maindoc.InlinedField)
			})
			t.Run("join (children)", func(t *testing.T) {
				is := require.New(t)
				childdoc := &childDoc{}
				for _, child := range childs {
					err := client.GetSource(child.id, child.parentID, childdoc)
					is.NoError(err)
					assertChild(is, child, childdoc)
				}
			})
		})
	}
	log.Print("Done")
}

func assertMain(is *require.Assertions, csvdoc *mainCSV, maindoc *mainDoc) {
	is.Equal(csvdoc.id, maindoc.ID)
	is.Equal(csvdoc.nested, maindoc.Nested)
	is.Equal(csvdoc.nonSearchableField, maindoc.NonSearchableField)
	is.Equal(csvdoc.text, maindoc.Text)
	// TextArray is hard to compare due to postgres array syntaxin csv
	is.Equal(maindoc.Join.Name, "immaparent")
	is.Empty(maindoc.Join.Parent)
	is.Equal(maindoc.DocType, "main")
}

func assertChild(is *require.Assertions, csvdoc *childCSV, maindoc *childDoc) {
	is.Equal(csvdoc.id, maindoc.ID)
	is.Equal(csvdoc.parentID, maindoc.ParentID)
	is.Equal(csvdoc.value, maindoc.Value)
	is.Equal(maindoc.Join.Name, "immachild")
	is.Equal(maindoc.Join.Parent, csvdoc.parentID)
	is.Equal(maindoc.DocType, "child")
	is.Empty(maindoc.IgnoreMe)
}

// csvData reads data from !!!sorted!!! CSV source files.
type csvData struct {
	main, child, inline *csv.Reader
	lastMain            *mainCSV
	lastChild           *childCSV
	lastInline          *inlineCSV
	mu                  sync.Mutex
}

// Files should be sorted by main ID
func newCSVData(main, child, inline io.Reader) *csvData {
	return &csvData{
		main:   csv.NewReader(main),
		child:  csv.NewReader(child),
		inline: csv.NewReader(inline),
	}
}

func (data *csvData) Next() (*mainCSV, []*childCSV, []*inlineCSV, error) {
	data.mu.Lock()
	defer data.mu.Unlock()

	main, err := data.main.Read()
	if err == io.EOF {
		return nil, nil, nil, io.EOF
	}
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "csv read")
	}

	data.lastMain = newMainCSV(main)
	var (
		childs  []*childCSV
		inlines []*inlineCSV
	)

	for {
		if data.lastChild != nil {
			if data.lastChild.parentID != data.lastMain.id {
				break
			}
			childs = append(childs, data.lastChild)
		}
		child, err := data.child.Read()
		if err != nil { // including EOF
			data.lastChild = nil
			break
		}
		data.lastChild = newChildCSV(child)
	}
	for {
		if data.lastInline != nil {
			if data.lastInline.parentID != data.lastMain.id {
				break
			}
			inlines = append(inlines, data.lastInline)
		}
		inline, err := data.inline.Read()
		if err != nil { // including EOF
			data.lastInline = nil
			break
		}
		data.lastInline = newInlineCSV(inline)
	}
	return data.lastMain, childs, inlines, nil
}

type searchClient struct {
	url string
	sem chan struct{}
}

func newClient(url, index string, concurrency int) *searchClient {
	// res := &searchClient{ }
	// res.url, err := url.Parse(url)
	// res.url.Path = path.Join(res.url.Path, index)
	return &searchClient{
		url: url + "/" + index + "/",
		sem: make(chan struct{}, concurrency),
	}
}

var ErrNotFound = errors.New("not found")

func (c *searchClient) GetSource(id, routing string, v interface{}) error {
	url := c.url + "_source/" + id + "?routing=" + routing
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrap(err, "http get")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%d %s: %s", resp.StatusCode, resp.Status, url)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return errors.Wrap(err, "json decode")
	}

	return nil
}

type nested struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type inlineDoc struct {
	ParentID string `json:"parent_id"`
	ID       string `json:"id"`
	Value    string `json:"value"`
}

// generated from main document json, via https://mholt.github.io/json-to-go/
type mainDoc struct {
	Nested             nested      `json:"nested"`
	InlinedField       []inlineDoc `json:"inlined_field"`
	NonSearchableField string      `json:"non_searchable_field"`
	Text               string      `json:"text"`
	TextArray          []string    `json:"text_array"`
	ID                 string      `json:"id"`
	Date               time.Time   `json:"date"`
	Deleted            bool        `json:"deleted"`
	Join               struct {
		Name   string `json:"name"`
		Parent string `json:"parent.omitempty"`
	} `json:"join"`
	DocType string `json:"docType"`
}

// generated from child document json, via https://mholt.github.io/json-to-go/
type childDoc struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Value    string `json:"value"`
	IgnoreMe string `json:"ignore_me"`
	Join     struct {
		Name   string `json:"name"`
		Parent string `json:"parent"`
	} `json:"join"`
	DocType string `json:"docType"`
}

type mainCSV struct {
	id, date, deleted, nonSearchableField, text, textArray, ignore_me string
	nested                                                            nested
}

func newMainCSV(row []string) *mainCSV {
	res := &mainCSV{
		id:                 row[0],
		date:               row[1],
		deleted:            row[2],
		nonSearchableField: row[4],
		text:               row[5],
		textArray:          row[6],
	}
	json.Unmarshal([]byte(row[3]), &res.nested)
	return res
}

type childCSV struct {
	id, parentID, value, ignoreMe string
}

func newChildCSV(row []string) *childCSV {
	return &childCSV{id: row[0], parentID: row[1], value: row[2], ignoreMe: row[3]}
}

type inlineCSV struct {
	id, parentID, value, ignoreMe string
}

func newInlineCSV(row []string) *inlineCSV {
	return &inlineCSV{id: row[0], parentID: row[1], value: row[2], ignoreMe: row[3]}
}
