package config

import "github.com/caarlos0/env/v11"

// Config holds all application configuration, loaded from environment variables.
type Config struct {
	Neo4jURI      string `env:"NEO4J_URI" envDefault:"bolt://localhost:7687"`
	Neo4jUser     string `env:"NEO4J_USER" envDefault:"neo4j"`
	Neo4jPassword string `env:"NEO4J_PASSWORD" envDefault:"changeme"`
	PostgresURL   string `env:"POSTGRES_URL" envDefault:"postgres://nodoplex:changeme@localhost:5432/nodoplex?sslmode=disable"`
	Port          string `env:"PORT" envDefault:"8080"`
}

// Load parses environment variables into a Config struct.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
