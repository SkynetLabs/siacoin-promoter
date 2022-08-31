package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/SkynetLabs/siacoin-promoter/api"
	"github.com/SkynetLabs/siacoin-promoter/database"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/node/api/client"
)

type (
	// config contains the configuration for the service which is parsed
	// from the environment vars.
	config struct {
		LogLevel   logrus.Level
		Port       int
		DBURI      string
		DBUser     string
		DBPassword string
		SkydOpts   client.Options
	}
)

const (
	// defaultSkydUserAgent defines the default agent used when no other
	// value is specified by the user.
	defaultSkydUserAgent = "Sia-Agent"

	// envAPIShutdownTimeout is the timeout for gracefully shutting down the
	// API before killing it.
	envAPIShutdownTimeout = 20 * time.Second

	// envMongoDBURI is the environment variable for the mongodb URI.
	envMongoDBURI = "MONGODB_URI"

	// envMongoDBUser is the environment variable for the mongodb user.
	envMongoDBUser = "MONGODB_USER"

	// envMongoDBPassword is the environment variable for the mongodb password.
	envMongoDBPassword = "MONGODB_PASSWORD"

	// envLogLevel is the environment variable for the log level used by
	// this service.
	envLogLevel = "SIACOIN_PROMOTER_LOG_LEVEL"

	// envSkydAPIAddr is the environment variable for setting the skyd
	// address.
	envSkydAPIAddr = "SKYD_API_ADDRESS"

	// envSkydAPIUserAgent is the environment variable for setting the skyd
	// User Agent.
	envSkydAPIUserAgent = "SKYD_API_USER_AGENT"

	// envSkydAPIPassword is the environment variable for setting the skyd
	// API password.
	// nolint:gosec // this is not a credential
	envSkydAPIPassword = "SKYD_API_PASSWORD"
)

// parseConfig parses a Config struct from the environment.
func parseConfig() (*config, error) {
	// Create config with default vars.
	cfg := &config{
		LogLevel: logrus.InfoLevel,
		SkydOpts: client.Options{
			UserAgent: defaultSkydUserAgent,
		},
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
	cfg.DBURI, ok = os.LookupEnv(envMongoDBURI)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", envMongoDBURI)
	}
	cfg.DBUser, ok = os.LookupEnv(envMongoDBUser)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", envMongoDBUser)
	}
	cfg.DBPassword, ok = os.LookupEnv(envMongoDBPassword)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", envMongoDBPassword)
	}
	cfg.SkydOpts.Address, ok = os.LookupEnv(envSkydAPIAddr)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", envSkydAPIAddr)
	}
	userAgent, ok := os.LookupEnv(envSkydAPIUserAgent)
	if ok {
		cfg.SkydOpts.UserAgent = userAgent
	}
	cfg.SkydOpts.Password, ok = os.LookupEnv(envSkydAPIPassword)
	if !ok {
		return nil, fmt.Errorf("%s wasn't specified", envSkydAPIPassword)
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

	// Connect to skyd.
	skydClient := client.New(cfg.SkydOpts)
	_, err = skydClient.DaemonReadyGet()
	if err != nil {
		logger.WithError(err).Fatal("Failed to connect to skyd")
	}

	// Create API.
	api, err := api.New(apiLogger, db, skydClient, cfg.Port)
	if err != nil {
		logger.WithError(err).Fatal("Failed to init API")
	}

	// Register handler for shutdown.
	var wg sync.WaitGroup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-sigChan

		// Log that we are shutting down.
		logger.Info("Caught stop signal. Shutting down...")

		// Shut down API with sane timeout.
		shutdownCtx, cancel := context.WithTimeout(ctx, envAPIShutdownTimeout)
		defer cancel()
		if err := api.Shutdown(shutdownCtx); err != nil {
			logger.WithError(err).Error("Failed to shut down api")
		}
	}()

	// Start serving API.
	err = api.ListenAndServe()
	if err != nil && !errors.Contains(err, http.ErrServerClosed) {
		logger.WithError(err).Error("ListenAndServe returned an error")
	}

	// Wait for the goroutine to finish before continuing with the remaining
	// shutdown procedures.
	wg.Wait()

	// Close database.
	if err := db.Close(); err != nil {
		logger.WithError(err).Fatal("Failed to close database gracefully")
	}
}
