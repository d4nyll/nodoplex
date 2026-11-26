package config

import "testing"

func TestLoadRetentionSettings(t *testing.T) {
	for _, name := range []string{"NEO4J_URI", "NEO4J_USER", "NEO4J_PASSWORD", "POSTGRES_URL"} {
		t.Setenv(name, "x")
	}
	cases := []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"the defaults", nil, true},
		{"targets of 1", map[string]string{
			"NODOPLEX_CHANGE_ENTRY_COUNT_TARGET": "1", "NODOPLEX_CHANGE_ENTRY_SIZE_TARGET": "1"}, true},
		{"a count target of 0", map[string]string{"NODOPLEX_CHANGE_ENTRY_COUNT_TARGET": "0"}, false},
		{"a size target of 0", map[string]string{"NODOPLEX_CHANGE_ENTRY_SIZE_TARGET": "0"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if (err == nil) != c.valid {
				t.Fatalf("got error %v, want valid %v", err, c.valid)
			}
			if c.name == "the defaults" && (cfg.ChangeEntryCountTarget != 1000000 || cfg.ChangeEntrySizeTarget != 1073741824) {
				t.Errorf("defaults: %+v", cfg)
			}
		})
	}
}
