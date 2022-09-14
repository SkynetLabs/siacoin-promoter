package promoter

import (
	"github.com/SkynetLabs/siacoin-promoter/client"
)

type (
	AccountsClient struct {
		*client.Client
	}

	AccountsHealthGET struct {
		DBAlive bool `json:"dbAlive"`
	}

	// AcountsUserGET defines a representation of the User struct returned
	// by all accounts handlers.
	AccountsUserGET struct {
		Sub string `bson:"sub" json:"sub"`
	}
)

func NewAccountsClient(address string) *AccountsClient {
	return &AccountsClient{
		Client: client.NewClient(address),
	}
}

func (ac *AccountsClient) Health() (ahg AccountsHealthGET, err error) {
	err = ac.GetJSON("/health", &ahg)
	return
}

func (ac *AccountsClient) UserSub(authorizationHeader string) (string, error) {
	var aug AccountsUserGET
	headers := map[string]string{
		"Authorization": authorizationHeader,
	}
	err := ac.GetJSONWithHeaders("/user", headers, &aug)
	return aug.Sub, err
}

func (p *Promoter) SubFromAuthorizationHeader(authHeader string) (string, error) {
	return p.staticAccounts.UserSub(authHeader)
}
