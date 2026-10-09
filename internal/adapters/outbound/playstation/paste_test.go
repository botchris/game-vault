package playstation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanNPSSO_pasted(t *testing.T) {
	const v = "Fx1aB2cD3eF4gH5iJ6kL7mN8oP9qR0sT1uV2wX3yZ4aB5cD6eF7gH8iJ9kL0mN1o"
	for _, pasted := range []string{
		v,
		`"` + v + `"`,
		`{"npsso":"` + v + `"}`,
		"npsso=" + v,
		"Cookie: a=1; npsso=" + v + "; b=2",
	} {
		assert.Equal(t, v, cleanNPSSO(pasted), pasted)
	}
}
