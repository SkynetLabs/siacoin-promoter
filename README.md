# Siacoin Promoter
The Siacoin Promoter is a payment integration for the Skynet stack. It is meant
to be used in combination with the
[promoter](https://github.com/SkynetLabs/siacoin-promoter).

It is a separate daemon that offers a simple API to receive a payment address
for a logged-in user which it will then watch for incoming transactions.
Incoming transactions will then be stored in a database, converted to credits
and sent to the promoter which is responsible for tracking a user's credit
balance. The conversion rate defaults to 1 SC == 1 Credit but can be configured
by updating the corresponding value in the mongo database directly. 

For efficiency reasons the Siacoin Promoter will create a pool of addresses and
then assign addresses to users on demand. That way we can minimize the amount of
blockchain syncing skyd needs to do. These addresses will then be added to all
skyd's within the cluster using skyd's `/wallet/watch` endpoint to make sure
each instance of skyd is aware of all incoming transactions.

## TODOs

- Endpoint that exposes the conversion rate to a UI
- HTTP call that notifies the promoter about the new transaction (The exact spot in the code is marked with TODO)

## Database Layout

The Siacoin Promoter will persist all data in a MongoDB database called `siacoin-promoter`.
It makes use of the following collections:

- `config` - contains the SC -> Credits conversion rate.
- `locks` - used for locking the 
- `watched_addresses` - contains a link between watched addresses, users and servers that created them
- `transactions` - contains all observed transactions and tracks whether the [promoter](https://github.com/SkynetLabs/siacoin-promoter) has been notified about them yet.

## Environment Variables

- `ACCOUNTS_HOST` - hostname or IP address of the accounts service
- `ACCOUNTS_PORT` - port that the accounts service listens on
- `MONGODB_URI` - URI of the Mongo database e.g. `mongodb://localhost:37017`
- `MONGODB_USER` - username of the Mongo database
- `MONGODB_PASSWORD` - password for the Mongo database
- `SIACOIN_PROMOTER_LOG_LEVEL` - one of `panic`, `fatal`, `error`, `warn`, `info`, `debug`,`trace` (Defaults to `info`)
- `SKYD_API_ADDRESS` - address that the skyd API listens on including port e.g. `http://localhost:9980`
- `SKYD_API_USER_AGENT` - user agent to use when connecting to skyd. (Defaults to `Sia-Agent`)
- `SIA_API_PASSWORD` - API password for skyd
- `SERVER_DOMAIN` - the server's unique domain within a cluster e.g. `eu-ger-1.siasky.net`

## API

### /health (GET)

The health endpoint pings both MongoDB and skyd to see if they response
successfully. The response contains a single boolean for each of those pings to
indicate whether it was successful.


#### Example Response
```json
{
	"dbalive": true,
	"skydalive": true,
}
```

### /address (POST)

The address endpoint returns an address for a logged in user. The request is
expected to have either the `Authorization` or `Cookie` header set. Using that,
the endpoint will obtain the user's sub from the
[accounts](https://github.com/SkynetLabs/skynet-accounts) service. Then it uses
the sub to either find an existing address for a user or assign a new one.
Subsequent calls to this endpoing should return the same address unless the
address was invalidated.

#### Example Response
```json
{
	"address": "134f52619b86ee5bfa9e64ad3b336986682777d42c7adfd671bebdf63d2fda07713035a91587",
}
```

### /dead/:servername (POST)

This endpoint allows for marking a server with the given name as dead. The name
matches the value set by `SERVER_DOMAIN` on the machine. It will mark all
addresses created by that server which are already assigned to users as invalid
and delete the ones that are not assigned yet from the pool. The next call to
the address endpoint will then return a fresh address for affected users.

NOTE: Make sure to back up the seed of the dead server. Users might accidentally
send money to invalidated addresses. The Siacoin Promoter will correctly assign
these values to the corresponding user but if the seed is lost, you can't spend
that value anymore.