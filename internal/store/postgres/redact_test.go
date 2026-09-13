package postgres

import "testing"

func TestRedact(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips user and password",
			input: "postgres://requestarr:s3cr3t@localhost:5432/requestarr?sslmode=disable",
			want:  "postgres://localhost:5432/requestarr?sslmode=disable",
		},
		{
			name:  "no credentials present",
			input: "postgres://localhost:5432/requestarr",
			want:  "postgres://localhost:5432/requestarr",
		},
		{
			name:  "unparseable input never echoed back",
			input: "postgres://user:pass@[::invalid",
			want:  "(unparseable database url)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.input); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
