package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("user home directory unavailable")
	}

	t.Setenv("PICOCLAW_TEST_PATH", filepath.Join("tmp", "quota.json"))

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: " ", want: ""},
		{name: "home", in: "~/.picoclaw/email-quotas.json", want: filepath.Join(home, ".picoclaw", "email-quotas.json")},
		{name: "env", in: "$PICOCLAW_TEST_PATH", want: filepath.Join("tmp", "quota.json")},
		{name: "absolute", in: "/tmp/email-quotas.json", want: "/tmp/email-quotas.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandPath(tt.in); got != tt.want {
				t.Fatalf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
