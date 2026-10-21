package domain

import "testing"

func TestValidatePlatformSelection(t *testing.T) {
	tests := []struct {
		name  string
		input PlatformSelection
		want  bool
	}{
		{"any only", PlatformSelection{Any: true}, true},
		{"explicit", PlatformSelection{PlatformKeys: []string{"pc", "ps5"}}, true},
		{"none", PlatformSelection{}, false},
		{"any plus explicit", PlatformSelection{Any: true, PlatformKeys: []string{"pc"}}, false},
		{"duplicate keys", PlatformSelection{PlatformKeys: []string{"pc", "pc"}}, false},
		{"empty key", PlatformSelection{PlatformKeys: []string{""}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidatePlatformSelection(tt.input); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
