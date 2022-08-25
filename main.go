package main

import (
	"context"
	"fmt"
	"os"

	"github.com/SkynetLabs/siacoin-promoter/api"
	"github.com/SkynetLabs/siacoin-promoter/database"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
)

type (
	Config struct {
		LogLevel   logrus.Level
		Port       int
		DBURI      string
		DBUser     string
		DBPassword string
	}
)

const (
	// mongoDBURI is the environment variable for the mongodb URI.
	mongoDBURI = "MONGODB_URI"

	// mongoDBUser is the environment variable for the mongodb user.
	mongoDBUser = "MONGODB_USER"

	// mongoDBPassword is the environment variable for the mongodb password.
	mongoDBPassword = "MONGODB_PASSWORD"

	// envLogLevel is the environment variable for the log level used by
	// this service.
	envLogLevel = "SIACOIN_PROMOTER_LOG_LEVEL"
)

// parseConfig parses a Config struct from the environment.
func parseConfig() (*Config, error) {
	// Create config with default vars.
	cfg := &Config{
		LogLevel: logrus.InfoLevel,
	}

	// Parse custom vars from environment.
	var ok bool
	var err error

	logLevelStr, ok := os.LookupEnv(envLogLevel)
	if ok {
		cfg.LogLevel, err = logrus.ParseLevel(logLevelStr)
		if err != nil {
			return nil, errors.AddContext(err, "failed to parse log level")
		}
	}
	cfg.DBURI, ok = os.LookupEnv(mongoDBURI)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", mongoDBURI)
	}
	cfg.DBUser, ok = os.LookupEnv(mongoDBUser)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", mongoDBUser)
	}
	cfg.DBPassword, ok = os.LookupEnv(mongoDBPassword)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", mongoDBPassword)
	}
	return cfg, nil
}

func main() {
	logger := logrus.New()

	// Create application context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Parse env vars.
	cfg, err := parseConfig()
	if err != nil {
		logger.WithError(err).Fatal("Failed to parse Config")
	}

	// Create the loggers for the submodules.
	logger.SetLevel(cfg.LogLevel)
	apiLogger := logger.WithField("modules", "api")

	// Connect to database.
	db, err := database.New(ctx, cfg.DBURI, cfg.DBUser, cfg.DBPassword)
	if err != nil {
		logger.WithError(err).Fatal("Failed to connect to database")
	}

	// Create API.
	api, err := api.New(apiLogger, db)
	if err != nil {
		logger.WithError(err).Fatal("Failed to init API")
	}

	err = api.ListenAndServe(cfg.Port)
	if err != nil {
		logger.WithError(err).Fatal("ListenAndServe returned an error")
	}
}
