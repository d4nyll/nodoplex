// Package config reads the server configuration from the environment (M1,
// D1).
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port          int64
	Neo4jURI      string
	Neo4jUser     string
	Neo4jPassword string
	PostgresURL   string

	ExposeChangefeed       bool
	ChangeEntryCountTarget int64
	ChangeEntrySizeTarget  int64
	DeleteOldChangeEntries bool
}

// Load reads each setting from its environment variable. It returns an error,
// and the server must not start, if a variable has a value that isn't valid,
// or if a setting with no default has no variable.
func Load() (*Config, error) {
	var c Config
	var err error
	if c.Port, err = integer("PORT", "8080", 1, 65535); err != nil {
		return nil, err
	}
	for _, s := range []struct {
		name string
		dest *string
	}{
		{"NEO4J_URI", &c.Neo4jURI},
		{"NEO4J_USER", &c.Neo4jUser},
		{"NEO4J_PASSWORD", &c.Neo4jPassword},
		{"POSTGRES_URL", &c.PostgresURL},
	} {
		v, ok := os.LookupEnv(s.name)
		if !ok || v == "" {
			return nil, fmt.Errorf("%s must be set to at least 1 character", s.name)
		}
		*s.dest = v
	}
	if c.ExposeChangefeed, err = boolean("NODOPLEX_EXPOSE_CHANGEFEED", "true"); err != nil {
		return nil, err
	}
	if c.DeleteOldChangeEntries, err = boolean("NODOPLEX_DELETE_OLD_CHANGE_ENTRIES", "true"); err != nil {
		return nil, err
	}
	if c.ChangeEntryCountTarget, err = integer("NODOPLEX_CHANGE_ENTRY_COUNT_TARGET", "1000000", 1, 1<<63-1); err != nil {
		return nil, err
	}
	if c.ChangeEntrySizeTarget, err = integer("NODOPLEX_CHANGE_ENTRY_SIZE_TARGET", "1073741824", 1, 1<<63-1); err != nil {
		return nil, err
	}
	return &c, nil
}

func lookup(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return def
}

func boolean(name, def string) (bool, error) {
	switch lookup(name, def) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%s must be true or false", name)
}

// integer reads one or more ASCII decimal digits, with a value from lo to hi.
func integer(name, def string, lo, hi int64) (int64, error) {
	v := lookup(name, def)
	for _, c := range []byte(v) {
		if c < '0' || c > '9' {
			v = ""
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if v == "" || err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, lo, hi)
	}
	return n, nil
}
