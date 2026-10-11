//go:build linux || darwin || freebsd

package diskspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatfs(t *testing.T) {
	free, total, err := Statfs{}.Space(t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, total)
	assert.LessOrEqual(t, free, total)
}
