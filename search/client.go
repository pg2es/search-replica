package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"go.uber.org/zap"
)

var (
	ErrBulkCommitFail = errors.New("commit bulk returned errors")
)

type Credentials struct {
	user     string
	password string
	set      bool
}

func NewClient(host, username, password string, logger *zap.Logger) (c *Client, err error) {
	c = &Client{
		Client: *http.DefaultClient,
		logger: logger,
	}

	// default scheme, before parsing. Otherwise domain name would be parsed as relative path url
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}

	c.Host, err = url.Parse(host)
	if err != nil {
		return nil, err
	}

	// Parse https://user:password@host/ credentials
	c.credentials.user = c.Host.User.Username()
	if username != "" {
		c.credentials.user = username
	}
	c.credentials.password, c.credentials.set = c.Host.User.Password()
	if password != "" {
		c.credentials.password = password
		c.credentials.set = true
	}

	c.Host.User = nil // remove `user:password@` part from host if any.

	return c, nil
}

type Client struct {
	http.Client
	credentials Credentials
	Host        *url.URL
	throttle    bool
	logger      *zap.Logger
}

// Do wraps default http.Client.Do with authorization
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c.credentials.set {
		req.SetBasicAuth(c.credentials.user, c.credentials.password)
	}
	return c.Client.Do(req)
}

// Bulk request with basic error handling
// XXX: When using the HTTP API, make sure that the client does not send HTTP chunks, as this will slow things down. See: https://www.elastic.co/guide/en/elasticsearch/reference/7.10/docs-bulk.html
func (c *Client) Bulk(body io.Reader) error {
	addr := c.Host.ResolveReference(&url.URL{
		Path:     "/_bulk",
		RawQuery: "filter_path=items.*.error,errors",
	})

	body = io.MultiReader(body, bytes.NewReader([]byte{'\n'})) // Additional "termination" newline means end of a batch

	req, err := http.NewRequest("POST", addr.String(), body)
	if err != nil {
		return fmt.Errorf("prepare bulk request: %w", err)
	}
	req.Header.Add("Content-Type", "application/x-ndjson")

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("execute bulk request: %w", err)
	}

	if resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			// return SLOW_DOWN error, so caller can adjust throtling
			c.throttle = true
		}

		if ce := c.logger.Check(zap.DebugLevel, "error response"); ce != nil {
			respBody, _ := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			// TODO: wrap body with json.RawMessage if response Content-Type is "application/json"
			ce.Write(zap.Any("body", respBody), zap.Int("status_code", resp.StatusCode))
		}

		return ErrHTTP{StatusCode: resp.StatusCode}
	}

	// Response is filtered out by `filter_path` query parameter
	respVal := BulkResponseErrors{}
	dec := json.NewDecoder(resp.Body)
	defer resp.Body.Close()
	if err := dec.Decode(&respVal); err != nil {
		return err
	}

	var returnErr error
	for _, err := range respVal.Errors {
		c.logger.Warn("push error", zap.String("_id", err.DocID), zap.String("type", err.Type), zap.String("reason", err.Reason))
		// TODO (#18): Make response error mapper:
		// - illegal_argument_exception wrong index mapping
		// - document_missing_exception ignore?
		// - cluster_block_exception - fatal, restart won't help
		// E.G:
		// Ignore update of previously deleted document
		if err.Type == "document_missing_exception" {
			continue
		}
		returnErr = ErrBulkCommitFail
	}

	return returnErr
}

func (c *Client) Script(id, source string) error {
	addr := c.Host.ResolveReference(&url.URL{
		Path: path.Join("/_scripts", id),
	})

	body := &bytes.Buffer{}
	body.WriteString(`{"script": {"lang": "painless", "source": `)
	if err := json.NewEncoder(body).Encode(source); err != nil {
		return fmt.Errorf("can not encode script request: %w", err)
	}
	body.WriteString(`}}`)

	req, err := http.NewRequest("PUT", addr.String(), body)
	if err != nil {
		return fmt.Errorf("prepare bulk request: %w", err)
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("script compilation request: %w", err)
	}

	// todo c.checkThrottle(resp), to encapsulate http.StatusTooManyRequests checks
	if resp.StatusCode >= 300 {
		return ErrHTTP{StatusCode: resp.StatusCode}
	}

	// XXX: do we need to check body here?

	return nil
}

// ErrHTTP is a wrapper on http status codes.
type ErrHTTP struct {
	StatusCode int
}

func (e ErrHTTP) Error() string {
	return http.StatusText(e.StatusCode)
}
