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
	}
)

const (
	// apiShutdownTimeout is the timeout for gracefully shutting down the
	// API before killing it.
	apiShutdownTimeout = 20 * time.Second

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
func parseConfig() (*config, error) {
	// Create config with default vars.
	cfg := &config{
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
	api, err := api.New(apiLogger, db, cfg.Port)
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
		shutdownCtx, cancel := context.WithTimeout(ctx, apiShutdownTimeout)
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
