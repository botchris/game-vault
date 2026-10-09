package humble

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanCookie_pasted(t *testing.T) {
	for pasted, want := range map[string]string{
		"abc123":                   "abc123",
		`"abc123"`:                 "abc123",
		"_simpleauth_sess=abc123;": "abc123",
		" abc123 \n":               "abc123",
		"csrf_cookie=x; _simpleauth_sess=abc123; y=2": "abc123",
		"Cookie: a=1; _simpleauth_sess=abc123":        "abc123",
	} {
		assert.Equal(t, want, cleanCookie(pasted), pasted)
	}
}
