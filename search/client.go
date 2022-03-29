package search

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/pkg/errors"
)

type Credentials struct {
	user     string
	password string
	set      bool
}

func NewClient(host, username, password string) (c *Client, err error) {
	c = &Client{
		Client: *http.DefaultClient,
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
		RawQuery: "filter_path=errors", // ?filter_path=items.*.error,errors
	})

	body = io.MultiReader(body, bytes.NewReader([]byte{'\n'})) // Additional "termination" newline means end of a batch

	req, err := http.NewRequest("POST", addr.String(), body)
	if err != nil {
		return errors.Wrap(err, "prepare bulk request")
	}
	req.Header.Add("Content-Type", "application/x-ndjson")

	resp, err := c.Do(req)
	if err != nil {
		return errors.Wrap(err, "bulk request")
	}

	if resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			// defer time.Sleep(5 * time.Second)
			// TODO: blocking throtling
			c.throttle = true
		}
		respBody, _ := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		log.Printf("%s", respBody)

		return ErrHTTP{StatusCode: resp.StatusCode}
	}

	// Response is filtered out by `filter_path` query parameter.Here we do not care about per-item results. Even in case of a single error, we loose consistency, so best case would be to fail and retry request, or restart script
	respVal := struct {
		Errors bool `json:"errors"`
		// Items - ignored; pottentialy can be used for stats and debug logging.
	}{}

	dec := json.NewDecoder(resp.Body)
	defer resp.Body.Close()
	if err := dec.Decode(&respVal); err != nil {
		return err
	}

	if respVal.Errors {
		return errors.New("commit bulk returned errors")
	}

	return nil
}

func (c *Client) Script(id, source string) error {
	addr := c.Host.ResolveReference(&url.URL{
		Path: path.Join("/_scripts", id),
	})

	body := &bytes.Buffer{}
	body.WriteString(`{"script": {"lang": "painless", "source": `)
	if err := json.NewEncoder(body).Encode(source); err != nil {
		return errors.Wrap(err, "can not encode script request")
	}
	body.WriteString(`}}`)

	req, err := http.NewRequest("PUT", addr.String(), body)
	if err != nil {
		return errors.Wrap(err, "prepare bulk request")
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return errors.Wrap(err, "script compilation request")
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
