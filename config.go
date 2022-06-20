package main

import (
	"flag"
	"log"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
)

var (
	pgSlotCreate   bool
	pgSlotReCreate bool
	reindex        bool
)

func init() {
	flag.BoolVar(&pgSlotCreate, "create", false, "Create new replication slot, if specified slot does not exists.")
	flag.BoolVar(&pgSlotReCreate, "recreate", false, "Deletes slot and creates new one.")
	flag.BoolVar(&reindex, "reindex", false, "Start with a backup to populate data into empty ES cluster (not implemented)")
}

// Config for the application.
type Config struct {
	Postgres struct {
		// Postgres replication slot. Used to controll WAL positions.
		Slot string `envconfig:"PG_SLOT" default:"pg2es"`
		// PostgresPublication containing databases and tables that should be replicated or indexed by the search engine.
		Publication string `envconfig:"PG_PUBLICATION" default:"search"`
		// PostgreSQL connection string
		// Host string `encvonfig:"PGHOST" required:"true"`
		Host string `envconfig:"PGHOST"`

		// Port is the port of the Postgres server.
		Port uint16 `envconfig:"PGPORT"`

		// Database to connect to.
		Database string `envconfig:"PGDATABASE"`

		// User to use to connect to the database.
		User string `envconfig:"PGUSER"`

		// Password for the user.
		Password string `envconfig:"PGPASSWORD"`

		// Further PG environment variables listed in
		// https://www.postgresql.org/docs/current/libpq-envars.html
		// are also accepted, but not listed here.
	}

	Search struct {
		// URL or host of ElasticSearch/OpenSearch. username and password can be defined here
		URL      string `envconfig:"SEARCH_HOST" required:"true"`
		User     string `envconfig:"SEARCH_USERNAME" required:"false"`
		Password string `envconfig:"SEARCH_PASSWORD" required:"false"`
		// BulkSizeLimit in Megabytes, limits request body size of bulk requests. Small values (2-8MB) are recommended. Extremely large requests do not improve performance, while causing extra memory pressure. Default elasticsearch limit is 100MB
		BulkSizeLimit int `envconfig:"SEARCH_BULK_SIZE" default:"4"`
		// PushInterval between bulk requests to the search engine.
		PushInterval time.Duration `envconfig:"SEARCH_PUSH_INTERVAL" default:"30s"`
	}

	// LogFormat [ json (default) | cli ]
	LogFormat string `envconfig:"LOG_FORMAT" default:"json"`
	LogLevel  string `envconfig:"LOG_LEVEL" default:"warn"`

	// Internal http API and metrics. Default 0.0.0.0:80
	Address string `envconfig:"ADDR"`
}

// FromEnv loads the configuration from environment variables. Panics if can not config us invalid
func FromEnv() *Config {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		log.Fatal(errors.Wrap(err, "Can not read initial config"))
	}
	return &cfg
}
