package source

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/errkind"
)

func TestSignedOut_kind(t *testing.T) {
	// Precondition, never Unauthenticated: the web page signs the user out of Game Vault on that.
	assert.Equal(t, errkind.Precondition, errkind.Of(fmt.Errorf("scan: %w", SignedOut("sign in again"))))
}
