package promoter

import (
	"net/http"

	"github.com/SkynetLabs/siacoin-promoter/client"
)

type (
	// AccountsClient wraps the helper client with account service specific
	// code.
	AccountsClient struct {
		*client.Client
	}

	// AccountsHealthGET defines the structure of the account service's
	// /health endpoint's response.
	AccountsHealthGET struct {
		DBAlive bool `json:"dbAlive"`
	}

	// AccountsUserGET defines a representation of the User struct returned
	// by all accounts handlers.
	AccountsUserGET struct {
		Sub string `bson:"sub" json:"sub"`
	}
)

// NewAccountsClient creates a new client to communicate with the accounts
// service API.
func NewAccountsClient(address string) *AccountsClient {
	return &AccountsClient{
		Client: client.NewClient(address),
	}
}

// Health calls the /health endpoint on the accounts service.
func (ac *AccountsClient) Health() (ahg AccountsHealthGET, err error) {
	err = ac.GetJSON("/health", &ahg)
	return
}

// UserSub uses the /user endpoint of the accounts service to return the user's
// sub.
func (ac *AccountsClient) UserSub(headers http.Header) (string, error) {
	var aug AccountsUserGET
	forwardedHeaders := map[string]string{
		"Authorization": headers.Get("Authorization"),
		"Cookie":        headers.Get("Cookie"),
	}
	err := ac.GetJSONWithHeaders("/user", forwardedHeaders, &aug)
	return aug.Sub, err
}

// SubFromAuthorizationHeader is a convenience method to expose the client's
// UserSub method through the promoter interface.
func (p *Promoter) SubFromAuthorizationHeader(headers http.Header) (string, error) {
	return p.staticAccounts.UserSub(headers)
}
