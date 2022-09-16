package promoter

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// staticCreateIndexes creates the necessary indexes for the siacoin promoter's db.
func (p *Promoter) staticCreateIndexes(ctx context.Context) error {
	// Let the lock client create its own indexes.
	err := p.staticLockClient.CreateIndexes(ctx)
	if err != nil {
		return err
	}
	// Create our own indexes.
	colIndexes := map[string][]mongo.IndexModel{
		colWatchedAddressesName: {
			{
				Keys:    bson.M{"primary": 1},
				Options: options.Index().SetName("primary"),
			},
			{
				Keys:    bson.M{"server": 1},
				Options: options.Index().SetName("server"),
			},
			{
				Keys:    bson.M{"user_id": 1},
				Options: options.Index().SetName("user_id"),
			},
		},
		colTransactionsName: {
			{
				Keys:    bson.M{"address_id": 1},
				Options: options.Index().SetName("address_id"),
			},
			{
				Keys:    bson.M{"credited": 1},
				Options: options.Index().SetName("credited"),
			},
			{
				Keys:    bson.M{"credited_at": 1},
				Options: options.Index().SetName("credited_at"),
			},
		},
	}
	for colName, idxs := range colIndexes {
		_, err = p.staticDB.Collection(colName).Indexes().CreateMany(ctx, idxs)
		if err != nil {
			return err
		}
	}
	return nil
}
