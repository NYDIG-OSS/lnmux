package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"
)

const (
	DefaultListenAddress          = "localhost:19090"
	DefaultInstrumentationAddress = "localhost:2112"

	PostgresDSNEnvKey = "LNMUX_PERSISTENCE_POSTGRES_DSN"
	IdentityKeyEnvKey = "LNMUX_IDENTITY_KEY"
)

type Config struct {
	// Lnd contains the configuration of the nodes.
	Lnd LndConfig `yaml:"lnd"`

	// DB contains the database config.
	DB DbConfig `yaml:"db"`

	// IdentityKey is the private key that is used to sign invoices.
	IdentityKey string `yaml:"identityKey"`

	// AutoSettle indicates that payments should be accepted automatically.
	AutoSettle bool `yaml:"autoSettle"`

	// ListenAddress is the network address that we listen on.
	ListenAddress string `yaml:"listenAddress"`

	// InstrumentationAddress is the network address that Prometheus
	// and eventually pprof and/or other instrumentation listen on.
	InstrumentationAddress string `yaml:"instrumentationAddress"`

	Logging LoggingConfig `yaml:"logging"`

	DistributedLock DistributedLockConfig `mapstructure:"distributedLock" json:"distributedLock" yaml:"distributedLock"`
}

// LoggingConfig contains options related to log outputs.
type LoggingConfig struct {
	// Level defined the minimum log level to be output
	Level string `yaml:"level"`

	// Format defined the output format:
	//  'console' will be human-readable
	//  'json' will be machine-readable.
	Format string `yaml:"format"`

	// WithCaller indicates if the caller is displayed in the log
	WithCaller bool `yaml:"withCaller"`

	// Enables grpc-level logging output.
	GrpcLogging bool `yaml:"grpcLogging"`

	// Enables grpc-level payload logging output.
	GrpcPayloadLogging bool `yaml:"grpcPayloadLogging"`
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

type DbConfig struct {
	// DSN is the connection string for the database.
	DSN string `yaml:"dsn"`

	// Maximum number of socket connections.
	// Default is 10 connections per every CPU as reported by runtime.NumCPU.
	PoolSize int `yaml:"poolSize"`

	// Minimum number of idle connections which is useful when establishing
	// new connection is slow.
	MinIdleConns int `yaml:"minIdleConns"`

	// Connection age at which client retires (closes) the connection.
	// It is useful with proxies like PgBouncer and HAProxy.
	// Default is to not close aged connections.
	MaxConnAge time.Duration `yaml:"maxConnAge"`

	// Time for which client waits for free connection if all
	// connections are busy before returning an error.
	// Default is 30 seconds if ReadTimeOut is not defined, otherwise,
	// ReadTimeout + 1 second.
	PoolTimeout time.Duration `yaml:"poolTimeout"`

	// Amount of time after which client closes idle connections.
	// Should be less than server's timeout.
	// Default is 5 minutes. -1 disables idle timeout check.
	IdleTimeout time.Duration `yaml:"idleTimeout"`
}

type DistributedLockConfig struct {
	// Namespace is the namespace where the lock lives in.
	Namespace string `mapstructure:"namespace" json:"namespace" yaml:"namespace"`

	// Name is the name of the lock. This should identify the
	// single-instance service.
	Name string `mapstructure:"name" json:"name" yaml:"name"`

	// ID is an identifier for this instance. If left empty, a random value
	// will be generated. If deployed, it is preferred to pass in the pod
	// name here.
	ID string `mapstructure:"id" json:"id" yaml:"id"`

	// DevKubeConfig is the path to a local kube config files ($HOME/.kube/config typically). Dev only.
	DevKubeConfig string `mapstructure:"devKubeConfig" json:"devKubeConfig" yaml:"devKubeConfig"`
}

func loadConfig(c *cli.Context) (*Config, error) {
	filename := c.String("config")
	nonStrictConfig := c.Bool(nonStrictConfigFlag.Name)

	yamlFile, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if nonStrictConfig {
		err = yaml.Unmarshal(yamlFile, &cfg)
	} else {
		err = yaml.UnmarshalStrict(yamlFile, &cfg)
	}
	if err != nil {
		return nil, err
	}

	// Check if the user provides an environment variable for PG DSN and load it
	loadStringEnvVariable(PostgresDSNEnvKey, &cfg.DB.DSN)

	// Check if the user provides an environment variable for the identity key and load it
	loadStringEnvVariable(IdentityKeyEnvKey, &cfg.IdentityKey)

	return &cfg, nil
}

func loadStringEnvVariable(key string, value *string) {
	if val, ok := os.LookupEnv(key); ok {
		log.Infof("Environment variable found: %q", key)
		*value = val
	}
}
