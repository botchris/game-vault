package system_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/health"
	"gamevault/internal/application/system"
)

// brokenBackup fails like a full disk.
type brokenBackup struct{}

func (brokenBackup) BackupTo(context.Context, string) error {
	return errors.New("write /config/backups/x.db: no space left on device")
}

func TestService_healthIssues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	now := time.Now()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	longAgo := system.Status{StartedAt: now.Add(-10 * 24 * time.Hour)}

	t.Run("GIVEN daily automatic backups whose last run failed", func(t *testing.T) {
		dir := t.TempDir()
		svc := system.NewService(nil, brokenBackup{}, nil, nil, time.Now, logger, longAgo, dir, 3)
		svc.ScheduleBackups(24 * time.Hour)
		require.Error(t, svc.ScheduledBackup(ctx))

		t.Run("WHEN the health issues are asked", func(t *testing.T) {
			issues := svc.Issues(ctx)

			t.Run("THEN the failure is an error with its cause, pointing to the backups", func(t *testing.T) {
				require.NotEmpty(t, issues)
				assert.Equal(t, health.LevelError, issues[0].Level)
				assert.Equal(t, health.ReasonBackupFailed, issues[0].Reason)
				assert.Empty(t, issues[0].Source, "named by the UI in its language")
				assert.Equal(t, health.KindBackups, issues[0].Kind)
				assert.Contains(t, issues[0].Message, "no space left on device")
				assert.Equal(t, health.TargetBackups, issues[0].Target)
			})
		})
	})

	t.Run("GIVEN daily automatic backups whose newest is four days old", func(t *testing.T) {
		dir := t.TempDir()
		writeBackup(t, dir, "gamevault-20261001-120000.db", now.Add(-4*24*time.Hour))
		svc := system.NewService(nil, fileBackup{}, nil, nil, time.Now, logger, longAgo, dir, 3)
		svc.ScheduleBackups(24 * time.Hour)

		t.Run("WHEN the health issues are asked", func(t *testing.T) {
			issues := svc.Issues(ctx)

			t.Run("THEN it warns that none succeeded lately", func(t *testing.T) {
				require.Len(t, issues, 1)
				assert.Equal(t, health.LevelWarning, issues[0].Level)
				assert.Equal(t, health.ReasonBackupStale, issues[0].Reason)
				assert.Equal(t, "4", issues[0].Params["days"])
				assert.Contains(t, issues[0].Message, "4 days")
			})
		})

		t.Run("WHEN an automatic backup then succeeds", func(t *testing.T) {
			require.NoError(t, svc.ScheduledBackup(ctx))

			t.Run("THEN all is well", func(t *testing.T) {
				assert.Empty(t, svc.Issues(ctx))
			})
		})
	})

	t.Run("GIVEN an automatic backup that failed, WHEN one is made by hand", func(t *testing.T) {
		dir := t.TempDir()
		failing := &switchBackup{err: errors.New("no space left on device")}
		svc := system.NewService(nil, failing, nil, nil, time.Now, logger, longAgo, dir, 3)
		svc.ScheduleBackups(24 * time.Hour)
		require.Error(t, svc.ScheduledBackup(ctx))
		require.Len(t, svc.Issues(ctx), 1)

		failing.err = nil
		_, err := svc.CreateBackup(ctx)
		require.NoError(t, err)

		t.Run("THEN the failure is cleared, as its message asked", func(t *testing.T) {
			assert.Empty(t, svc.Issues(ctx))
		})

		t.Run("AND a manual backup that fails counts too", func(t *testing.T) {
			failing.err = errors.New("disk full")
			_, err := svc.CreateBackup(ctx)
			require.Error(t, err)

			issues := svc.Issues(ctx)
			require.Len(t, issues, 1)
			assert.Equal(t, health.ReasonBackupFailed, issues[0].Reason)
		})
	})

	t.Run("GIVEN a server restarted today whose newest backup is four days old", func(t *testing.T) {
		dir := t.TempDir()
		writeBackup(t, dir, "gamevault-20261001-120000.db", now.Add(-4*24*time.Hour))
		svc := system.NewService(nil, fileBackup{}, nil, nil, time.Now, logger, system.Status{StartedAt: now.Add(-time.Hour)}, dir, 3)
		svc.ScheduleBackups(24 * time.Hour)

		t.Run("THEN it still warns: the restart made no backup", func(t *testing.T) {
			issues := svc.Issues(ctx)
			require.Len(t, issues, 1)
			assert.Equal(t, health.ReasonBackupStale, issues[0].Reason)
		})
	})

	t.Run("GIVEN a fresh server without backups yet", func(t *testing.T) {
		svc := system.NewService(nil, fileBackup{}, nil, nil, time.Now, logger, system.Status{StartedAt: now.Add(-time.Hour)}, t.TempDir(), 3)
		svc.ScheduleBackups(24 * time.Hour)

		t.Run("THEN it is not stale: the schedule has not come round yet", func(t *testing.T) {
			assert.Empty(t, svc.Issues(ctx))
		})
	})

	t.Run("GIVEN automatic backups turned off, and an old backup", func(t *testing.T) {
		dir := t.TempDir()
		writeBackup(t, dir, "gamevault-20261001-120000.db", now.Add(-30*24*time.Hour))
		svc := system.NewService(nil, fileBackup{}, nil, nil, time.Now, logger, longAgo, dir, 3)

		t.Run("THEN nothing is expected, so nothing is reported", func(t *testing.T) {
			assert.Empty(t, svc.Issues(ctx))
		})
	})
}

// switchBackup fails with err, or writes the copy when nil.
type switchBackup struct{ err error }

func (b *switchBackup) BackupTo(ctx context.Context, path string) error {
	if b.err != nil {
		return b.err
	}

	return fileBackup{}.BackupTo(ctx, path)
}
