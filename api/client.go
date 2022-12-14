package api

import (
	"fmt"

	"github.com/SkynetLabs/siacoin-promoter/client"
	"go.sia.tech/siad/types"
)

// PromoterClient provides a library for communicating with the promoter's API.
type PromoterClient struct {
	*client.Client
}

// NewClient creates a new PromoterClient.
func NewClient(addr string) *PromoterClient {
	return &PromoterClient{
		Client: client.NewClient(addr),
	}
}

// Address returns the active address for a given user to send money to. The
// user is identified by the specified authentication header which should
// contain a valid JWT.
func (c *PromoterClient) Address(headers map[string]string) (types.UnlockHash, error) {
	var uap UserAddressPOST
	err := c.Client.PostJSONWithHeaders("/address", headers, &uap)
	return uap.Address, err
}

// MarkServerDead calls the /server/:servername endpoint to mark a server as
// dead within the db.
func (c *PromoterClient) MarkServerDead(server string) error {
	return c.Client.Post(fmt.Sprintf("/dead/%s", server))
}

// Health calls the /health endpoint on the server.
func (c *PromoterClient) Health() (hg HealthGET, err error) {
	err = c.GetJSON("/health", &hg)
	return
}
