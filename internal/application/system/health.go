package system

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gamevault/internal/application/health"
)

// backupStaleFloor is the shortest silence that makes automatic backups look stalled, however short
// their interval.
const backupStaleFloor = 3 * 24 * time.Hour

// Issues implements health.Check for automatic backups. While they are on, the last backup having
// failed (automatic or made by hand) is an error, and no backup in twice their interval, three days
// at least, a warning. The age is the newest backup file's, or the server's start when there is
// none yet (a fresh install has had no chance to make one). It reads the backups directory only.
func (s *Service) Issues(ctx context.Context) []health.Issue {
	s.mu.Lock()
	every, lastErr, lastErrAt := s.backupEvery, s.lastBackupErr, s.lastBackupErrAt
	s.mu.Unlock()

	if every <= 0 {
		return nil
	}

	issue := health.Issue{
		Key:    "backups",
		Kind:   health.KindBackups,
		Target: health.TargetBackups,
	}

	if lastErr != nil {
		issue.Level, issue.Reason, issue.Since = health.LevelError, health.ReasonBackupFailed, lastErrAt
		issue.Message = fmt.Sprintf("the last backup failed: %v: check the disk space and the log, then make one now in System", lastErr)

		return []health.Issue{issue}
	}

	newest := s.status.StartedAt
	if list, err := s.ListBackups(ctx); err == nil && len(list) > 0 {
		newest = list[0].CreatedAt
	}

	quiet := s.now().Sub(newest)
	if newest.IsZero() || quiet <= max(2*every, backupStaleFloor) {
		return nil
	}

	days := strconv.Itoa(int(quiet.Hours() / 24))
	issue.Level, issue.Reason, issue.Since = health.LevelWarning, health.ReasonBackupStale, newest
	issue.Params = map[string]string{"days": days}
	issue.Message = fmt.Sprintf("no backup has succeeded in %s days: check the log, or make one now in System", days)

	return []health.Issue{issue}
}
