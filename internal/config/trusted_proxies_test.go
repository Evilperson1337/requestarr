package config

import (
	"net/netip"
	"strings"
	"testing"
)

func TestTrustedProxiesAreEmptyByDefault(t *testing.T) {
	t.Setenv("REQUESTARR_TRUSTED_PROXIES", "")
	c := FromEnv()
	if got := c.TrustedProxyPrefixes(); len(got) != 0 {
		t.Fatalf("default trusts %v", got)
	}
	if err := c.ValidateBootstrap(); err != nil && strings.Contains(err.Error(), "REQUESTARR_TRUSTED_PROXIES") {
		t.Fatalf("unexpected %v", err)
	}
}

func TestTrustedProxiesParseCIDRsAndSingleAddresses(t *testing.T) {
	t.Setenv("REQUESTARR_TRUSTED_PROXIES", " 10.0.0.0/8 , 192.168.1.5,fd00::/8 ")
	c := FromEnv()
	if err := c.ValidateBootstrap(); err != nil && strings.Contains(err.Error(), "REQUESTARR_TRUSTED_PROXIES") {
		t.Fatalf("unexpected %v", err)
	}
	got := c.TrustedProxyPrefixes()
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32"), netip.MustParsePrefix("fd00::/8")}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestInvalidTrustedProxiesNameTheVariableAndNeverEchoTheValue(t *testing.T) {
	for _, bad := range []string{"not-a-cidr-secretvalue", "10.0.0.0/33", "10.0.0.0/8,,", "10.0.0.0/8;1.2.3.4"} {
		t.Setenv("REQUESTARR_TRUSTED_PROXIES", bad)
		err := FromEnv().ValidateBootstrap()
		if err == nil || !strings.Contains(err.Error(), "REQUESTARR_TRUSTED_PROXIES") {
			t.Fatalf("%q: err=%v, want one naming the variable", bad, err)
		}
		if strings.Contains(err.Error(), "secretvalue") || strings.Contains(err.Error(), "1.2.3.4") || strings.Contains(err.Error(), "/33") {
			t.Fatalf("the error echoes the value: %v", err)
		}
	}
}
