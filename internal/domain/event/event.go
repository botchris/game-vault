// Package event names what Game Vault tells the outside world about: things that happened to the
// collection, things that need the user, and security notices. Kinds are stored in subscriptions
// and in the notification queue, so their strings never change.
package event

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"gamevault/internal/domain/game"
)

// Kind names an event. Values are stored: never rename one.
type Kind string

// The event kinds, in the order the UI lists them.
const (
	KindCollectionAdded  Kind = "collection.added"
	KindReleaseAnnounced Kind = "release.announced"
	KindReleaseOut       Kind = "release.out"
	KindHistoricalLow    Kind = "deal.historical_low"
	KindIssueOpened      Kind = "issue.opened"
	KindIssueResolved    Kind = "issue.resolved"
	KindKeysExpiring     Kind = "keys.expiring"
	KindFailedLogins     Kind = "auth.failed_logins"
)

// Group sorts kinds for the UI.
type Group string

// Values of Group.
const (
	GroupActivity  Group = "activity"
	GroupAttention Group = "attention"
	GroupSecurity  Group = "security"
)

var catalog = []struct {
	kind  Kind
	group Group
}{
	{KindCollectionAdded, GroupActivity},
	{KindReleaseAnnounced, GroupActivity},
	{KindReleaseOut, GroupActivity},
	{KindHistoricalLow, GroupActivity},
	{KindIssueOpened, GroupAttention},
	{KindIssueResolved, GroupAttention},
	{KindKeysExpiring, GroupAttention},
	{KindFailedLogins, GroupSecurity},
}

// Kinds returns every kind, in display order.
func Kinds() []Kind {
	out := make([]Kind, 0, len(catalog))
	for _, c := range catalog {
		out = append(out, c.kind)
	}

	return out
}

// GroupOf returns a kind's group, "" for an unknown kind.
func GroupOf(k Kind) Group {
	for _, c := range catalog {
		if c.kind == k {
			return c.group
		}
	}

	return ""
}

// Known reports whether k is a kind of the catalog.
func Known(k Kind) bool { return slices.Contains(Kinds(), k) }

// Event is one thing to tell: its kind, when it happened and its payload (one of the types below,
// matching the kind).
type Event struct {
	// ID identifies the event in the queue.
	ID string

	// Kind says what happened.
	Kind Kind

	// At is when it happened.
	At time.Time

	// Data is the payload: CollectionAdded for KindCollectionAdded, and so on.
	Data any
}

// New returns an event with a new id.
func New(kind Kind, at time.Time, data any) Event {
	return Event{
		ID:   uuid.Must(uuid.NewV7()).String(),
		Kind: kind,
		At:   at,
		Data: data,
	}
}

// Origin of games that entered the collection.
type Origin string

// Values of Origin. A source scan's origin is the source's name, in OriginName.
const (
	OriginSource Origin = "source"
	OriginManual Origin = "manual"
	OriginImport Origin = "import"
)

// GameRef is a game named in an event.
type GameRef struct {
	// ID is the game's id.
	ID game.ID `json:"id"`

	// Title is its title.
	Title string `json:"title"`

	// Systems are the systems of its copies, sorted.
	Systems []string `json:"systems"`
}

// CollectionAdded is KindCollectionAdded's payload.
type CollectionAdded struct {
	// Origin says where the games came from.
	Origin Origin `json:"origin"`

	// OriginName is the source's name for OriginSource, empty otherwise.
	OriginName string `json:"originName"`

	// Games are the games created, in the order they were created.
	Games []GameRef `json:"games"`

	// CopiesAdded counts copies added to games the user already had.
	CopiesAdded int `json:"copiesAdded"`
}

// Release is KindReleaseAnnounced's and KindReleaseOut's payload.
type Release struct {
	// Title is the release's title.
	Title string `json:"title"`

	// Date is when it comes out (YYYY-MM-DD), "" when not announced.
	Date game.Date `json:"date"`

	// Platforms are the platforms it comes out on.
	Platforms []string `json:"platforms"`

	// CoverURL is its public cover, "" without one.
	CoverURL string `json:"coverUrl"`

	// URL is its page on the database that announced it.
	URL string `json:"url"`

	// Because is the owned game that explains why the user hears about it.
	Because game.ID `json:"because"`

	// BecauseTitle is that game's title.
	BecauseTitle string `json:"becauseTitle"`
}

// Money is an amount in a currency's minor unit (cents for EUR), as game.Money, with stable JSON
// names: payloads are stored and sent to webhooks, so they do not follow the domain's renames.
type Money struct {
	// Amount is in the currency's minor unit.
	Amount int64 `json:"amount"`

	// Currency is the ISO 4217 code.
	Currency string `json:"currency"`
}

// HistoricalLow is KindHistoricalLow's payload: the deal as the provider gave it, in fields of its
// own so the stored JSON keeps its names whatever happens to game.Deal.
type HistoricalLow struct {
	// ProviderGameID is the deals service's own id of the game.
	ProviderGameID string `json:"providerGameId"`

	// Title is the game's title.
	Title string `json:"title"`

	// Shop names the shop that sells it.
	Shop string `json:"shop"`

	// Price is what it costs now.
	Price Money `json:"price"`

	// Regular is what it costs when not on sale.
	Regular Money `json:"regular"`

	// Cut is the discount, in percent of the regular price.
	Cut int `json:"cut"`

	// HistoricalLow reports that the price is the lowest the service has seen.
	HistoricalLow bool `json:"historicalLow"`

	// Expires is when the deal ends; zero (and left out) when the shop does not say.
	Expires time.Time `json:"expires,omitzero"`

	// URL opens the deal through the service's link.
	URL string `json:"url"`

	// ImageURL is the game's art on the service, "" without one.
	ImageURL string `json:"imageUrl"`
}

// HistoricalLowOf returns the payload telling about deal d.
func HistoricalLowOf(d game.Deal) HistoricalLow {
	return HistoricalLow{
		ProviderGameID: d.ProviderGameID,
		Title:          d.Title,
		Shop:           d.Shop,
		Price:          moneyOf(d.Price),
		Regular:        moneyOf(d.Regular),
		Cut:            d.Cut,
		HistoricalLow:  d.HistoricalLow,
		Expires:        d.Expires,
		URL:            d.URL,
		ImageURL:       d.ImageURL,
	}
}

func moneyOf(m game.Money) Money {
	return Money{
		Amount:   m.Amount,
		Currency: m.Currency,
	}
}

// Issue is KindIssueOpened's and KindIssueResolved's payload: a Health issue, in plain data.
type Issue struct {
	// Key identifies the issue (Health's key).
	Key string `json:"key"`

	// Level is "error" or "warning".
	Level string `json:"level"`

	// Kind is the issue's category, as Health names it.
	Kind string `json:"kind"`

	// Source names the provider or source; empty for backups and disk.
	Source string `json:"source"`

	// Reason is Health's reason code.
	Reason string `json:"reason"`

	// Params fill the reason's text.
	Params map[string]string `json:"params"`

	// Message is the English detail.
	Message string `json:"message"`

	// Since is when the problem began.
	Since time.Time `json:"since"`

	// Target is the page that fixes it.
	Target string `json:"target"`

	// TargetID names the item on that page.
	TargetID string `json:"targetId"`
}

// ExpiringKey is one key of KeysExpiring.
type ExpiringKey struct {
	// Game is the key's game.
	Game GameRef `json:"game"`

	// Store is where the key redeems (the copy's platform).
	Store string `json:"store"`

	// RedeemBy is the deadline.
	RedeemBy game.Date `json:"redeemBy"`

	// DaysLeft is how many days remain (0: today).
	DaysLeft int `json:"daysLeft"`
}

// KeysExpiring is KindKeysExpiring's payload, nearest deadline first.
type KeysExpiring struct {
	// Keys are the keys to tell about.
	Keys []ExpiringKey `json:"keys"`
}

// FailedLogins is KindFailedLogins's payload.
type FailedLogins struct {
	// Address is the client address.
	Address string `json:"address"`

	// Count is how many sign-ins failed in the window.
	Count int `json:"count"`

	// First is the earliest attempt.
	First time.Time `json:"first"`

	// Last is the latest attempt.
	Last time.Time `json:"last"`
}
