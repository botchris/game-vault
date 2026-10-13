package media

import "gamevault/internal/application/unattended"

// SetRhythm replaces the background scan's random source and sleep, so tests control its timing.
func (s *Service) SetRhythm(r unattended.Rhythm) { s.rhythm = r }
