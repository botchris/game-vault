package notify

import "context"

// Round runs one pass of the dispatcher over the due deliveries.
func (s *Service) Round(ctx context.Context) { s.round(ctx) }
