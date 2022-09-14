package api

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"gitlab.com/NebulousLabs/errors"
)

// Client is a library for interacting with the Siacoin Promoter's API.
type Client struct {
	staticAddr string
}

// NewClient creates a new Client for an API listening on the given address.
func NewClient(addr string) *Client {
	return &Client{
		staticAddr: addr,
	}
}

// readAPIError decodes and returns an api.Error.
func readAPIError(r io.Reader) error {
	var apiErr Error
	b, _ := ioutil.ReadAll(r)
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&apiErr); err != nil {
		return errors.AddContext(err, "could not read error response")
	}
	return apiErr
}

// get performs a GET request on the provided resource.
func (c *Client) get(resource string) (*http.Response, error) {
	return http.DefaultClient.Get(c.staticAddr + resource)
}

// getJSON performs a GET request on the provided resource and tries to json
// decode the response body into the provided object.
func (c *Client) getJSON(resource string, obj interface{}) error {
	resp, err := c.get(resource)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check for 200 since we expect a successful response with body.
	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp.Body)
	}

	dec := json.NewDecoder(resp.Body)
	return dec.Decode(obj)
}

// Health calls the /health endpoint on the server.
func (c *Client) Health() (hg HealthGET, err error) {
	err = c.getJSON("/health", &hg)
	return
}
