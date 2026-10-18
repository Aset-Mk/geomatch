package main

import "testing"

func TestLoadLoadgenConfig(t *testing.T) {
	tests := []struct {
		name, key, val string
		wantErr        bool
	}{
		{"valid drivers", "DRIVERS", "500", false},
		{"not a number", "DRIVERS", "abc", true},
		{"zero drivers", "DRIVERS", "0", true},
		{"bad duration", "UPDATE_PERIOD", "1x", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.val)
			cfg, err := loadLoadgenConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && cfg.Drivers != 500 {
				t.Errorf("Drivers = %d, want 500", cfg.Drivers)
			}
		})
	}
}
