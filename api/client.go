package api

import "github.com/SkynetLabs/siacoin-promoter/client"

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

// Health calls the /health endpoint on the server.
func (c *PromoterClient) Health() (hg HealthGET, err error) {
	err = c.GetJSON("/health", &hg)
	return
}
