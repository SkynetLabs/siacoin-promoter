package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
)

// TestParseConfig is a unit test for parseConfig.
func TestParseConfig(t *testing.T) {
	// Testing helpers.
	//
	// Sets the environment to sane values
	uri, user, password, logLevel := "URI", "user", "password", logrus.ErrorLevel
	setEnv := func() {
		err1 := os.Setenv(mongoDBURI, uri)
		err2 := os.Setenv(mongoDBUser, user)
		err3 := os.Setenv(mongoDBPassword, password)
		err4 := os.Setenv(envLogLevel, logLevel.String())
		if err := errors.Compose(err1, err2, err3, err4); err != nil {
			t.Fatal(err)
		}
	}
	// Asserts a parsed config against provided values.
	errParseFailed := errors.New("parseConfig failed")
	assertConfig := func(uri, user, password string, logLevel logrus.Level) error {
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
		return nil
	}

	// Environment cleanup.
	defer func() {
		err1 := os.Unsetenv(mongoDBURI)
		err2 := os.Unsetenv(mongoDBUser)
		err3 := os.Unsetenv(mongoDBPassword)
		err4 := os.Unsetenv(envLogLevel)
		if err := errors.Compose(err1, err2, err3, err4); err != nil {
			t.Fatal(err)
		}
	}()

	// Case 1: Sane values.
	setEnv()
	if err := assertConfig(uri, user, password, logLevel); err != nil {
		t.Fatal(err)
	}

	// Case 2: No log level.
	setEnv()
	if err := os.Unsetenv(envLogLevel); err != nil {
		t.Fatal(err)
	}
	if err := assertConfig(uri, user, password, logrus.InfoLevel); err != nil {
		t.Fatal(err)
	}

	// Case 3: No URI.
	setEnv()
	if err := os.Unsetenv(mongoDBURI); err != nil {
		t.Fatal(err)
	}
	err := assertConfig(uri, user, password, logLevel)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 4: No user.
	setEnv()
	if err := os.Unsetenv(mongoDBUser); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, logLevel)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}

	// Case 5: No password.
	setEnv()
	if err := os.Unsetenv(mongoDBPassword); err != nil {
		t.Fatal(err)
	}
	err = assertConfig(uri, user, password, logLevel)
	if !errors.Contains(err, errParseFailed) {
		t.Fatal(err)
	}
}
