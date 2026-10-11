package source

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSignedOut(t *testing.T) {
	err := SignedOut("the store did not accept the cookies: sign in again and paste them")

	assert.ErrorIs(t, err, ErrSignedOut)
	assert.ErrorIs(t, fmt.Errorf("library: %w", err), ErrSignedOut, "still matched when wrapped")
	assert.Equal(t, "the store did not accept the cookies: sign in again and paste them", err.Error())
	assert.NotErrorIs(t, errors.New("timeout"), ErrSignedOut)
}
