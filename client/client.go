package client

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"gitlab.com/NebulousLabs/errors"
)

type (
	// Error is the error type returned by the API in case the status code
	// is not a 2xx code.
	Error struct {
		Message string `json:"message"`
	}

	// Client is a helper library for interacting with an API.
	Client struct {
		staticAddr string
	}
)

// Error implements the error interface for the Error type. It returns only the
// Message field.
func (err Error) Error() string {
	return err.Message
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

// do attaches the given headers to a request and then executes it using the
// default client.
func (c *Client) do(req *http.Request, headers map[string]string) (*http.Response, error) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return http.DefaultClient.Do(req)
}

// get performs a GET request on the provided resource.
func (c *Client) get(resource string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest("GET", c.staticAddr+resource, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, headers)
}

// post performs a POST request on the provided resource.
func (c *Client) post(resource string, headers map[string]string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", c.staticAddr+resource, body)
	if err != nil {
		return nil, err
	}
	return c.do(req, headers)
}

// GetJSONWithHeaders performs a GET request on the provided resource and tries
// to json decode the response body into the provided object.
func (c *Client) GetJSONWithHeaders(resource string, headers map[string]string, obj interface{}) error {
	resp, err := c.get(resource, headers)
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

// GetJSON performs a GET request on the provided resource and tries to json
// decode the response body into the provided object.
func (c *Client) GetJSON(resource string, obj interface{}) error {
	return c.GetJSONWithHeaders(resource, nil, obj)
}

// PostJSONWithHeaders performs a POST request o the provided resource.
func (c *Client) PostJSONWithHeaders(resource string, headers map[string]string, obj interface{}) error {
	resp, err := c.post(resource, headers, nil)
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

// Post performs a simple post request to the resource without a body and
// without expecting a response.
func (c *Client) Post(resource string) error {
	resp, err := c.post(resource, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check for 200 since we expect a successful response with body.
	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp.Body)
	}
	return nil
}
