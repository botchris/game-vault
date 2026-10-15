package game

import "time"

// Deal is a shop's current price for a game, as a deals service reports it. Its prices and link are
// the service's, unchanged: its terms forbid altering them (affiliate tags included).
type Deal struct {
	// ProviderGameID is the deals service's own id of the game.
	ProviderGameID string

	Title string

	// SteamAppID is the game's Steam app, "" when the service knows none.
	SteamAppID string

	// Shop names the shop that sells it ("Steam", "GOG"...).
	Shop string

	// Price is what it costs now; Regular what it costs when not on sale.
	Price   Money
	Regular Money

	// Cut is the discount, in percent of the regular price (0–100).
	Cut int

	// HistoricalLow reports that the price is the lowest the service has seen for the game.
	HistoricalLow bool

	// Expires is when the deal ends; zero when the shop does not say.
	Expires time.Time

	// URL opens the deal (through the service's link); "" when it gave none usable.
	URL string

	// ImageURL is the game's art on the service; "" when there is none.
	ImageURL string

	// OwnedOn lists the systems the user owns the game on, when they own it only on consoles (never
	// Other, a copy without a platform). The home page fills it; providers leave it empty.
	OwnedOn []string
}

// Release is a game that comes out soon, as a game database reports it.
type Release struct {
	// ProviderGameID is the database's own id of the game.
	ProviderGameID string

	Title string

	// Date is the first release date (YYYY-MM-DD).
	Date Date

	// Platforms are the Game Vault systems it is announced for, known ones only.
	Platforms []string

	// CoverURL is its box art on the database; URL its page there. Either may be "".
	CoverURL string
	URL      string

	// Because is the owned game whose series it continues, and BecauseTitle that game's title;
	// both empty for a release listed for its anticipation alone.
	Because      ID
	BecauseTitle string
}
