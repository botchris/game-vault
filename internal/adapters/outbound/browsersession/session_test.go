package browsersession

import (
	"fmt"
	"testing"
)

func TestParse(t *testing.T) {
	cases := map[string]map[string]string{
		"Cookie: SESSION=abc; BA-tassadar=xyz": {"BA-tassadar": "xyz", "SESSION": "abc"},
		"sid=abc":                              {"sid": "abc"},
		"SESSION\tabc\taccount.battle.net\nBA-tassadar\txyz": {"BA-tassadar": "xyz", "SESSION": "abc"},
		"   ": {},
	}
	for in, want := range cases {
		if got := Parse(in); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("Parse(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRenewedCookiesFollowThePaste(t *testing.T) {
	pasted := "sid=old; remid=r"
	st := Remember(pasted, "", map[string]string{"sid": "new"})
	if c := Current(pasted, st); c["sid"] != "new" || c["remid"] != "r" {
		t.Fatalf("current: %v", c)
	}
	if c := Current("sid=pasted-again", st); c["sid"] != "pasted-again" {
		t.Fatalf("a new paste must win over older renewals: %v", c)
	}
	if Remember(pasted, st, nil) != st {
		t.Fatal("nothing renewed: state unchanged")
	}
}
