// Package home gathers the home page's external blocks: current deals and upcoming releases, which
// change from one day to the next. They come from providers configured like any other
// (provider.KindDeals, provider.KindReleases), gathered by an unattended, jittered job and stored as
// one snapshot per block, so the page loads at once, the services get few requests, and a failure
// shows the last good data with its age.
package home

import (
	"context"
	"time"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// DealList names one of the deal lists the home page shows.
type DealList string

// Values of DealList.
const (
	DealsFeatured       DealList = "featured"        // the service's own selection of current deals
	DealsHistoricalLows DealList = "historical_lows" // deals at the lowest price the service has seen
)

// DealQuery asks a deals provider for one list.
type DealQuery struct {
	List DealList

	// Country is the ISO 3166-1 alpha-2 code whose prices to list ("US", "DE"); a provider's own
	// country setting wins over it.
	Country string

	// Limit is how many deals to return at most.
	Limit int
}

// DealsProvider lists current deals (provider.KindDeals). It knows nothing of the collection: the
// home service removes the games the user owns.
type DealsProvider interface {
	providers.Impl

	// Deals returns the list q asks for, best first. Errors that concern the provider (a rejected
	// key, a rate limit, an outage) match the provider sentinels.
	Deals(ctx context.Context, settings schema.Settings, q DealQuery) ([]game.Deal, error)
}

// OwnedRef is an owned game as a releases provider needs it to find the series it belongs to.
type OwnedRef struct {
	Game  game.ID
	Title string

	// Links are the game's store links (automatic ones included); a provider uses the ones it can
	// look up.
	Links game.Links
}

// ReleasesProvider lists upcoming games (provider.KindReleases).
type ReleasesProvider interface {
	providers.Impl

	// Anticipated returns the most anticipated games that come out within window from now, most
	// anticipated first.
	Anticipated(ctx context.Context, settings schema.Settings, window time.Duration) ([]game.Release, error)

	// Sequels returns the upcoming games of the series (franchises and collections) the owned games
	// belong to, soonest first, each with the owned game that explains it (Release.Because).
	Sequels(ctx context.Context, settings schema.Settings, owned []OwnedRef) ([]game.Release, error)
}

// Block names one of the home page's external blocks.
type Block string

// Values of Block.
const (
	BlockDeals    Block = "deals"
	BlockReleases Block = "releases"
)

// Blocks returns the external blocks, in the order the job looks at them.
func Blocks() []Block { return []Block{BlockDeals, BlockReleases} }

// ErrorKind classifies a failed refresh, for a message the page translates.
type ErrorKind string

// Values of ErrorKind.
const (
	ErrorRateLimited ErrorKind = "rate_limited" // the service asked to slow down
	ErrorQuota       ErrorKind = "quota"        // the service's allowance is used up
	ErrorSettings    ErrorKind = "settings"     // the service rejects the key or settings
	ErrorUnavailable ErrorKind = "unavailable"  // the service is not answering
	ErrorFailed      ErrorKind = "failed"       // anything else
)

// Snapshot is what a block's last refreshes left: the last good data, and the last failure if it
// came after it.
type Snapshot struct {
	Block Block

	// Featured and HistoricalLows are the deals block's lists, as the provider gave them (the owned
	// games are removed when the page is read, so a purchase shows at once).
	Featured       []game.Deal
	HistoricalLows []game.Deal

	// FromSeries and Anticipated are the releases block's lists.
	FromSeries  []game.Release
	Anticipated []game.Release

	// FetchedAt is when the data was gathered; zero when no refresh has succeeded yet.
	FetchedAt time.Time

	// Error, ErrorKind and FailedAt describe the last failed refresh; empty when the last attempt
	// succeeded.
	Error     string
	ErrorKind ErrorKind
	FailedAt  time.Time

	// AttemptedAt is when the last refresh that asked a provider and was interrupted on the caller's
	// side started (a closed page, a server shutting down). It is not a failure of the service, so
	// the block shows nothing of it, but it counts for the 15-minute guard: the request reached the
	// service.
	AttemptedAt time.Time
}

// Snapshots is the persistence port of the blocks' snapshots.
type Snapshots interface {
	// Snapshot returns a block's snapshot, and false when none was stored (or it can no longer be
	// read: the block is then gathered again).
	Snapshot(ctx context.Context, b Block) (Snapshot, bool, error)

	// SaveSnapshot stores a block's snapshot in place of the previous one.
	SaveSnapshot(ctx context.Context, s Snapshot) error
}
