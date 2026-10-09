package fanatical

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToken_pasted(t *testing.T) {
	const tok = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig"
	for _, pasted := range []string{
		tok,
		`"` + tok + `"`,
		" " + tok + " \n",
		`{"authenticated":true,"token":"` + tok + `","email":"x@example.test"}`,
	} {
		assert.Equal(t, tok, token(pasted), pasted)
	}
}
