package game

import (
	"slices"
	"strings"
	"time"
)

// Kind says how a copy is owned.
type Kind string

const (
	KindKey      Kind = "key"      // a redeemable CD key
	KindLibrary  Kind = "library"  // already in an account library (Steam, Ubisoft Connect...)
	KindPhysical Kind = "physical" // a disc or cartridge
)

// Status is the lifecycle state of a copy. Which values are valid depends on the Kind.
type Status string

const (
	StatusUnrevealed Status = "unrevealed"
	StatusRevealed   Status = "revealed"
	StatusRedeemed   Status = "redeemed"
	StatusGifted     Status = "gifted"
	StatusExpired    Status = "expired"
	StatusOwned      Status = "owned"
	StatusLent       Status = "lent"
	StatusSold       Status = "sold"
)

var statusesByKind = map[Kind][]Status{
	KindKey:      {StatusUnrevealed, StatusRevealed, StatusRedeemed, StatusGifted, StatusExpired},
	KindLibrary:  {StatusOwned},
	KindPhysical: {StatusOwned, StatusLent, StatusSold},
}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { _, ok := statusesByKind[k]; return ok }

// DefaultStatus is the status a new copy of this kind gets when none is given.
func (k Kind) DefaultStatus() Status { return statusesByKind[k][0] }

// Allows reports whether s is a valid status for kind k.
func (k Kind) Allows(s Status) bool { return slices.Contains(statusesByKind[k], s) }

// CopyDetails are the descriptive attributes of a copy, set by the user or by a source.
type CopyDetails struct {
	Kind       Kind
	Platform   string // store, launcher or console: "Steam", "PS4"...
	Status     Status
	Key        string // CD key or gift link (keys only)
	RedeemBy   Date   // redeem deadline (keys only)
	Origin     string // bundle, shop, gift...
	AcquiredOn Date
	Edition    string
	Condition  string  // physical only
	Location   string  // physical only
	Barcode    Barcode // physical only: EAN/UPC printed on the box
	Notes      string
}

// normalize trims values, fills defaults and enforces the kind/status invariants.
func (d CopyDetails) normalize() (CopyDetails, error) {
	for _, s := range []*string{&d.Platform, &d.Key, &d.Origin, &d.Edition, &d.Condition, &d.Location, &d.Notes} {
		*s = strings.TrimSpace(*s)
	}
	if !d.Kind.Valid() {
		return d, invalid("copy kind %q is not valid", d.Kind)
	}
	if d.Status == "" {
		d.Status = d.Kind.DefaultStatus()
	}
	if !d.Kind.Allows(d.Status) {
		return d, invalid("status %q is not valid for a %s copy", d.Status, d.Kind)
	}
	d.Platform = CanonicalPlatform(d.Platform)
	if d.Kind != KindKey {
		d.Key, d.RedeemBy = "", ""
	}
	if d.Kind != KindPhysical {
		d.Barcode = ""
	}
	return d, nil
}

// Copy is one owned instance of a game. It is an entity inside the Game aggregate.
type Copy struct {
	ID ID
	CopyDetails
	SourceID   string // source that manages this copy; empty for manual copies
	ExternalID string // stable identifier inside the source
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// IsPendingKey reports whether the copy is a key you still have to do something with.
func (c Copy) IsPendingKey() bool {
	return c.Kind == KindKey && (c.Status == StatusUnrevealed || c.Status == StatusRevealed)
}

// applyImport overlays details coming from a source onto the copy. Values the source does not
// provide are kept, and a key the user marked as redeemed is never moved back to pending.
func (c *Copy) applyImport(in CopyDetails) bool {
	before := c.CopyDetails
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	c.Kind = in.Kind
	set(&c.Platform, in.Platform)
	set(&c.Key, in.Key)
	set(&c.Origin, in.Origin)
	set(&c.Edition, in.Edition)
	set(&c.Condition, in.Condition)
	set(&c.Location, in.Location)
	if in.Barcode != "" {
		c.Barcode = in.Barcode
	}
	set(&c.Notes, in.Notes)
	if in.RedeemBy != "" {
		c.RedeemBy = in.RedeemBy
	}
	if in.AcquiredOn != "" {
		c.AcquiredOn = in.AcquiredOn
	}
	keepRedeemed := before.Status == StatusRedeemed && (in.Status == StatusUnrevealed || in.Status == StatusRevealed)
	if in.Status != "" && !keepRedeemed {
		c.Status = in.Status
	}
	if !c.Kind.Allows(c.Status) {
		c.Status = c.Kind.DefaultStatus()
	}
	return c.CopyDetails != before
}
