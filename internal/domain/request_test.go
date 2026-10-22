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

func TestCanTransitionAndOpenStatuses(t *testing.T) {
	for _, tc := range []struct {
		from, to RequestStatus
		want     bool
	}{
		{RequestRequested, RequestApproved, true},
		{RequestRequested, RequestDenied, true},
		{RequestRequested, RequestCancelled, true},
		{RequestApproved, RequestCancelled, true},
		{RequestApproved, RequestApproved, false},
		{RequestApproved, RequestDenied, false},
		{RequestDenied, RequestCancelled, false},
		{RequestCancelled, RequestCancelled, false},
		{RequestAvailable, RequestCancelled, false},
		{RequestRequested, RequestAvailable, false},
	} {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%s,%s)=%v want %v", tc.from, tc.to, got, tc.want)
		}
	}
	for status, open := range map[RequestStatus]bool{RequestRequested: true, RequestApproved: true, RequestDenied: false, RequestAvailable: false, RequestCancelled: false} {
		if got := status.IsOpen(); got != open {
			t.Errorf("%s.IsOpen()=%v want %v", status, got, open)
		}
	}
}
