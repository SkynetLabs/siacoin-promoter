package promoter

import "github.com/SkynetLabs/siacoin-promoter/client"

type AccountsClient struct {
	*client.Client
}

type AccountsHealthGET struct {
	DBAlive bool `json:"dbAlive"`
}

func NewAccountsClient(address string) *AccountsClient {
	return &AccountsClient{
		Client: client.NewClient(address),
	}
}

func (ac *AccountsClient) Health() (ahg AccountsHealthGET, err error) {
	err = ac.GetJSON("/health", &ahg)
	return
}
