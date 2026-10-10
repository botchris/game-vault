package game

import (
	"slices"
	"strings"
	"time"
)

// Kind says how a copy is owned.
type Kind string

// Values of Kind.
const (
	KindKey      Kind = "key"      // a redeemable CD key
	KindLibrary  Kind = "library"  // already in an account library (Steam, Ubisoft Connect...)
	KindPhysical Kind = "physical" // a disc or cartridge
)

// Status is the lifecycle state of a copy. Which values are valid depends on the Kind.
type Status string

// Values of Status. KindKey copies use the first five, library copies only StatusOwned and
// physical copies StatusOwned, StatusLent and StatusSold.
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
	Grade      Grade    // physical only
	Contents   Contents // physical only
	Location   string   // physical only
	Barcode    Barcode  // physical only: EAN/UPC printed on the box
	Price      Money    // what was paid, any kind
	Notes      string
}

// normalize trims values, fills defaults and enforces the kind/status invariants.
func (d CopyDetails) normalize() (CopyDetails, error) {
	for _, s := range []*string{&d.Platform, &d.Key, &d.Origin, &d.Edition, &d.Location, &d.Notes} {
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

	if !d.Grade.Valid() {
		return d, invalid("grade %q is not valid", d.Grade)
	}

	if !d.Contents.valid() {
		return d, invalid("the copy's contents are not valid")
	}

	price, err := d.Price.normalize()
	if err != nil {
		return d, err
	}

	d.Price = price

	if d.Kind != KindPhysical {
		d.Barcode, d.Grade, d.Contents, d.Location = "", "", 0, ""
	}

	return d, nil
}

// Copy is one owned instance of a game. It is an entity inside the Game aggregate.
type Copy struct {
	ID ID
	CopyDetails

	// Photos are the user's pictures of the copy, in the order they chose.
	Photos []Photo

	// Estimates are the latest second-hand prices, one per provider, sorted by provider. Only
	// physical copies with a barcode have them.
	Estimates []Estimate

	// Fields are the copy's custom field values, by field id.
	Fields FieldValues

	// NextValuation is when the prices are next estimated; zero when none is planned.
	NextValuation time.Time

	// ValuedAt is when the prices were last asked, whatever the sources answered; zero when never.
	ValuedAt   time.Time
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
	set(&c.Location, in.Location)

	if in.Barcode != "" {
		c.Barcode = in.Barcode
	}

	if in.Grade != "" {
		c.Grade = in.Grade
	}

	if in.Contents != 0 {
		c.Contents = in.Contents
	}

	if !in.Price.IsZero() {
		c.Price = in.Price
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

	// Estimates belong to the product the barcode names.
	if c.Kind != KindPhysical || c.Barcode != before.Barcode {
		c.clearValuation()
	}

	return c.CopyDetails != before
}
