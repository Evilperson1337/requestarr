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

func TestPlatformSelectionSatisfiedByEvidenceOnAnySelectedPlatform(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sel      PlatformSelection
		evidence []string
		want     bool
	}{
		{"any, one platform", PlatformSelection{Any: true}, []string{"igdb:130"}, true},
		{"any, platform unknown to the library", PlatformSelection{Any: true}, []string{""}, true},
		{"any, no evidence at all", PlatformSelection{Any: true}, nil, false},
		{"explicit, selected platform present", PlatformSelection{PlatformKeys: []string{"igdb:167", "igdb:130"}}, []string{"igdb:130"}, true},
		{"explicit, OR across several selected platforms", PlatformSelection{PlatformKeys: []string{"igdb:167", "igdb:130"}}, []string{"igdb:19", "igdb:167"}, true},
		{"explicit, only other platforms present", PlatformSelection{PlatformKeys: []string{"igdb:167"}}, []string{"igdb:19", "igdb:130"}, false},
		{"explicit, platform unknown cannot satisfy", PlatformSelection{PlatformKeys: []string{"igdb:167"}}, []string{""}, false},
		{"explicit, no evidence", PlatformSelection{PlatformKeys: []string{"igdb:167"}}, nil, false},
		{"empty selection behaves as any", PlatformSelection{}, []string{"igdb:19"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sel.SatisfiedBy(tc.evidence); got != tc.want {
				t.Fatalf("SatisfiedBy(%v)=%v want %v", tc.evidence, got, tc.want)
			}
		})
	}
}
