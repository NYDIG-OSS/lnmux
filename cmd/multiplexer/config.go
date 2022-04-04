package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/yaml.v2"
)

type Config struct {
	// Lnd contains the configuration of the nodes.
	Lnd LndConfig

	// DSN is the connection string for the database.
	DSN string

	// IdentityKey is the private key that is used to sign invoices.
	IdentityKey string `yaml:"identityKey"`
}

func (c *Config) GetIdentityKey() ([32]byte, error) {
	keySlice, err := hex.DecodeString(c.IdentityKey)
	if err != nil {
		return [32]byte{}, fmt.Errorf("invalid identity key: %v", err)
	}
	if len(keySlice) != 32 {
		return [32]byte{}, errors.New("invalid identity key length")
	}

	var key [32]byte
	copy(key[:], keySlice)

	return key, nil
}

type LndConfig struct {
	Nodes []struct {
		// PubKey is the public key of this node.
		PubKey string `yaml:"pubKey"`

		// MacaroonPath is the disk path to Bottle's LND node's macaroon file
		MacaroonPath string `yaml:"macaroonPath"`

		// TlsCertPath is the disk path to Bottle's LND's TLS certificate file
		TlsCertPath string `yaml:"tlsCertPath"`

		// LndUrl is the URL and port pointing to Bottle's LND node
		LndUrl string `yaml:"lndUrl"`

		// Priority expressed the preference to use this node. Higher means higher preference.
		Priority int `yaml:"priority"`
	} `yaml:"nodes"`

	// Network is the bitcoin network that the connector is running on. Options: mainnet, testnet, regtest.
	Network string `yaml:"network"`

	// Timeout is a generic time limit waiting for calls to lnd to complete
	Timeout time.Duration `yaml:"timeout"`
}

func loadConfig(filename string) (*Config, error) {
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.UnmarshalStrict(yamlFile, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
