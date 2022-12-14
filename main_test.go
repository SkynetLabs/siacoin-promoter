package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/node/api/client"
)

// TestParseConfig is a unit test for parseConfig.
func TestParseConfig(t *testing.T) {
	// Testing helpers.
	//
	// Sets the environment to sane values
	uri, user, password, logLevel, serverDomain := "URI", "user", "password", logrus.ErrorLevel, "server.com"
	accountHost, accountPort := "127.0.0.1", "1234"
	opts := client.Options{
		Address:   ":9980",
		UserAgent: "agent",
		Password:  "pw",
	}
	setEnv := func() {
		err1 := os.Setenv(envMongoDBURI, uri)
		err2 := os.Setenv(envMongoDBUser, user)
		err3 := os.Setenv(envMongoDBPassword, password)
		err4 := os.Setenv(envLogLevel, logLevel.String())
		err5 := os.Setenv(envSkydAPIAddr, opts.Address)
		err6 := os.Setenv(envSkydAPIUserAgent, opts.UserAgent)
		err7 := os.Setenv(envSiaAPIPassword, opts.Password)
		err8 := os.Setenv(envServerDomain, serverDomain)
		err9 := os.Setenv(envAccountsHost, accountHost)
		err10 := os.Setenv(envAccountsPort, accountPort)
		if err := errors.Compose(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10); err != nil {
			t.Fatal(err)
		}
	}
	// Asserts a parsed config against provided values.
	errParseFailed := errors.New("parseConfig failed")
	assertConfig := func(uri, user, password, serverDomain, accountHost, accountPort string, logLevel logrus.Level, skydOpts client.Options) error {
		cfg, err := parseConfig()
		if err != nil {
			return errParseFailed
		}
		if cfg.DBURI != uri {
			return fmt.Errorf("uri mismatch: %v != %v", cfg.DBURI, uri)
		}
		if cfg.DBUser != user {
			return fmt.Errorf("user mismatch: %v != %v", cfg.DBUser, user)
		}
		if cfg.DBPassword != password {
			return fmt.Errorf("password mismatch: %v != %v", cfg.DBPassword, password)
		}
		if cfg.LogLevel != logLevel {
			return fmt.Errorf("logLevel mismatch: %v != %v", cfg.LogLevel, logLevel)
		}
		if cfg.SkydOpts.Address != skydOpts.Address {
			return fmt.Errorf("skydOpt.Address mismatch: %v != %v", cfg.SkydOpts.Address, skydOpts.Address)
		}
		if cfg.SkydOpts.UserAgent != skydOpts.UserAgent {
			return fmt.Errorf("skydOpt.UserAgent mismatch: %v != %v", cfg.SkydOpts.UserAgent, skydOpts.UserAgent)
		}
		if cfg.SkydOpts.Password != skydOpts.Password {
			return fmt.Errorf("skydOpt.Password mismatch: %v != %v", cfg.SkydOpts.Password, skydOpts.Password)
		}
		if cfg.ServerDomain != serverDomain {
			return fmt.Errorf("skydOpt.ServerDomain mismatch: %v != %v", cfg.ServerDomain, serverDomain)
		}
		if accountsAddr := fmt.Sprintf("%s:%s", accountHost, accountPort); cfg.AccountsAPIAddr != accountsAddr {
			return fmt.Errorf("skydOpt.AccountsAPIAddr mismatch: %v != %v", cfg.AccountsAPIAddr, accountsAddr)
		}
		return nil
	}

	// Environment cleanup.
	defer func() {
		err1 := os.Unsetenv(envMongoDBURI)
		err2 := os.Unsetenv(envMongoDBUser)
		err3 := os.Unsetenv(envMongoDBPassword)
		err4 := os.Unsetenv(envLogLevel)
		err5 := os.Unsetenv(envSkydAPIAddr)
		err6 := os.Unsetenv(envSkydAPIUserAgent)
		err7 := os.Unsetenv(envSiaAPIPassword)
		err8 := os.Unsetenv(envServerDomain)
		err9 := os.Unsetenv(envAccountsHost)
		err10 := os.Unsetenv(envAccountsPort)
		if err := errors.Compose(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10); err != nil {
			t.Fatal(err)
		}
	}()

	// Case 1: Sane values.
	setEnv()
	if err := assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, opts); err != nil {
		t.Fatal(err)
	}

	// Case 2: No log level.
	setEnv()
	if err := os.Unsetenv(envLogLevel); err != nil {
		t.Fatal(err)
	}
	if err := assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logrus.InfoLevel, opts); err != nil {
		t.Fatal(err)
	}

	// Case 3: No URI.
	setEnv()
	if err := os.Unsetenv(envMongoDBURI); err != nil {
		t.Fatal(err)
	}
	err := assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 4: No user.
	setEnv()
	if err := os.Unsetenv(envMongoDBUser); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 5: No password.
	setEnv()
	if err := os.Unsetenv(envMongoDBPassword); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 5: No skyd address.
	setEnv()
	if err := os.Unsetenv(envSkydAPIAddr); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 6: No skyd agent.
	setEnv()
	if err := os.Unsetenv(envSkydAPIUserAgent); err != nil {
		t.Fatal(err)
	}
	optsNoAgent := opts
	optsNoAgent.UserAgent = defaultSkydUserAgent
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, optsNoAgent)
	if err != nil {
		t.Fatal(err)
	}

	// Case 7: No skyd password.
	setEnv()
	if err := os.Unsetenv(envSiaAPIPassword); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 8: No server domain.
	setEnv()
	if err := os.Unsetenv(envServerDomain); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logrus.InfoLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 9: No accounts host.
	setEnv()
	if err := os.Unsetenv(envAccountsHost); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logrus.InfoLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 10: No accounts port.
	setEnv()
	if err := os.Unsetenv(envAccountsPort); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, serverDomain, accountHost, accountPort, logrus.InfoLevel, opts)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}
}
