package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestService_order(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN checks reporting warnings and errors of several kinds", func(t *testing.T) {
		s := NewService(
			CheckFunc(func(context.Context) []Issue {
				return []Issue{
					{
						Key:    "provider:b",
						Level:  LevelWarning,
						Kind:   KindProvider,
						Source: "TheGamesDB",
					},
					{
						Key:    "provider:a",
						Level:  LevelError,
						Kind:   KindProvider,
						Source: "IGDB",
					},
				}
			}),
			nil,
			CheckFunc(func(context.Context) []Issue {
				return []Issue{
					{
						Key:    "source:1",
						Level:  LevelError,
						Kind:   KindSource,
						Source: "Epic",
					},
					{
						Key:    "backups",
						Level:  LevelWarning,
						Kind:   KindBackups,
						Source: "Backups",
					},
				}
			}),
			CheckFunc(func(context.Context) []Issue { return nil }),
		)

		t.Run("WHEN the issues are listed", func(t *testing.T) {
			got := s.Issues(ctx)

			t.Run("THEN errors come first, then by kind and source", func(t *testing.T) {
				keys := make([]string, 0, len(got))
				for _, i := range got {
					keys = append(keys, i.Key)
				}

				assert.Equal(t, []string{"provider:a", "source:1", "backups", "provider:b"}, keys)
			})
		})
	})
}

// fakeDisk reports a fixed free and total size.
type fakeDisk struct {
	free  uint64
	total uint64
	err   error
}

func (d fakeDisk) Space(string) (uint64, uint64, error) { return d.free, d.total, d.err }

func TestDiskCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	const gb, mb = uint64(1 << 30), uint64(1 << 20)

	for _, tc := range []struct {
		name string
		disk fakeDisk
		want Level // "" for no issue
	}{
		{"plenty of room", fakeDisk{
			free:  50 * gb,
			total: 500 * gb,
		}, ""},
		{"under 1 GB free", fakeDisk{
			free:  800 * mb,
			total: 10 * gb,
		}, LevelWarning},
		{"exactly 1 GB free on a small disk", fakeDisk{
			free:  1 * gb,
			total: 10 * gb,
		}, ""},
		{"under 5% of a mid-size disk", fakeDisk{
			free:  5 * gb,
			total: 200 * gb,
		}, LevelWarning},
		{"under 5% of a huge disk, but over 10 GB", fakeDisk{
			free:  199 * gb,
			total: 8000 * gb,
		}, ""},
		{"under 10 GB on a huge disk", fakeDisk{
			free:  9 * gb,
			total: 8000 * gb,
		}, LevelWarning},
		{"exactly 200 MB free", fakeDisk{
			free:  200 * mb,
			total: 2 * gb,
		}, LevelWarning},
		{"under 200 MB free", fakeDisk{
			free:  150 * mb,
			total: 500 * gb,
		}, LevelError},
		{"a filesystem that reports no size", fakeDisk{
			free:  0,
			total: 0,
		}, ""},
		{"a system that cannot tell", fakeDisk{err: ErrUnsupported}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DiskCheck{
				Path:  "/config",
				Space: tc.disk,
			}.Issues(ctx)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}

			if assert.Len(t, got, 1) {
				assert.Equal(t, tc.want, got[0].Level)
				assert.Equal(t, KindDisk, got[0].Kind)
				assert.Contains(t, got[0].Message, "/config")
				assert.NotEmpty(t, got[0].Params["free"])
				assert.NotEmpty(t, got[0].Params["percent"])

				want := ReasonDiskLow
				if tc.want == LevelError {
					want = ReasonDiskCritical
				}

				assert.Equal(t, want, got[0].Reason)
			}
		})
	}

	t.Run("without a disk reader the check is off", func(t *testing.T) {
		assert.Empty(t, DiskCheck{Path: "/config"}.Issues(ctx))
	})
}
