package config

import "testing"

func TestLoadInvalidLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debgu")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted unknown level, want error")
	}
}
