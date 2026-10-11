// Package diskspace reads how much room a filesystem has, for the health check of the config
// directory (see health.DiskCheck).
package diskspace

import "gamevault/internal/application/health"

// Statfs implements health.DiskSpace with the operating system's filesystem statistics.
type Statfs struct{}

var _ health.DiskSpace = Statfs{}
