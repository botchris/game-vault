// Package game holds the catalog domain: the Game aggregate and the copies you own of it.
package game

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Game is the aggregate root of the catalog. A game is registered once and every owned
// instance of it (a Humble key, a Steam library entry, a PS4 disc...) is a Copy inside it.
// All changes to copies go through the Game so its invariants hold.
type Game struct {
	id         ID
	title      string
	links      Links
	notes      string
	playStatus PlayStatus
	rating     Rating
	fields     FieldValues
	covers     map[string]EditionCover
	mainSystem string
	copies     []Copy
	createdAt  time.Time
	updatedAt  time.Time
}

// New creates a game with no copies.
func New(title string, now time.Time) (*Game, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, invalid("title is required")
	}

	return &Game{
		id:        NewID(),
		title:     title,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// Info holds a game's own editable attributes.
type Info struct {
	Title string

	// Links are the stores the game is linked to (see Links).
	Links Links
	Notes string

	// PlayStatus says whether the user means to play the game, is playing it or is done with it.
	PlayStatus PlayStatus

	// Rating is the user's score, 0 when unrated.
	Rating Rating

	// Fields are the game's custom field values, by field id.
	Fields FieldValues
}

func (i Info) normalize() (Info, error) {
	i.Title, i.Notes = strings.TrimSpace(i.Title), strings.TrimSpace(i.Notes)
	if i.Title == "" {
		return i, invalid("title is required")
	}

	links, err := i.Links.normalize()
	if err != nil {
		return i, err
	}

	i.Links = links.clone()

	if !i.PlayStatus.Valid() {
		return i, invalid("play status %q is not valid", i.PlayStatus)
	}

	if !i.Rating.Valid() {
		return i, invalid("the rating must be between 0 and %d", MaxRating)
	}

	return i, nil
}

// Rehydrate rebuilds a game from storage. Only repositories should call it.
func Rehydrate(id ID, info Info, copies []Copy, createdAt, updatedAt time.Time) *Game {
	// Empty values are never stored, on copies as on the game, so the mutators that count values
	// (RemoveFieldValues) never count an empty one.
	copies = slices.Clone(copies)
	for i := range copies {
		copies[i].Fields = copies[i].Fields.compact()
	}

	return &Game{
		id:         id,
		title:      info.Title,
		links:      info.Links.clone(),
		notes:      info.Notes,
		playStatus: info.PlayStatus,
		rating:     info.Rating,
		fields:     info.Fields.compact(),
		copies:     copies,
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	}
}

// ID returns the game's identifier.
func (g *Game) ID() ID { return g.id }

// Title returns the game's title.
func (g *Game) Title() string { return g.title }

// Links returns the stores the game is linked to. The map is a copy.
func (g *Game) Links() Links { return g.links.clone() }

// Notes returns the user's free-form notes about the game.
func (g *Game) Notes() string { return g.notes }

// PlayStatus returns where the user is with the game, PlayNone when they have not said.
func (g *Game) PlayStatus() PlayStatus { return g.playStatus }

// Rating returns the user's score for the game, 0 when unrated.
func (g *Game) Rating() Rating { return g.rating }

// CreatedAt returns when the game was registered.
func (g *Game) CreatedAt() time.Time { return g.createdAt }

// UpdatedAt returns when the game or its copies were last changed.
func (g *Game) UpdatedAt() time.Time { return g.updatedAt }

// MatchKey returns the normalized title used to consolidate the same game across sources.
func (g *Game) MatchKey() string { return MatchKey(g.title) }

// Copies returns a copy of the game's copies, so callers cannot bypass the aggregate.
func (g *Game) Copies() []Copy {
	out := make([]Copy, len(g.copies))
	for i, c := range g.copies {
		out[i] = c.clone()
	}

	return out
}

// Info returns the game's own attributes.
func (g *Game) Info() Info {
	return Info{
		Title:      g.title,
		Links:      g.links.clone(),
		Notes:      g.notes,
		PlayStatus: g.playStatus,
		Rating:     g.rating,
		Fields:     g.fields.compact(),
	}
}

// UpdateInfo changes the game's own attributes. It reports whether the links changed: covers and
// details depend on them, so cached ones can be refreshed.
func (g *Game) UpdateInfo(i Info, now time.Time) (linksChanged bool, err error) {
	i, err = i.normalize()
	if err != nil {
		return false, err
	}

	linksChanged = !i.Links.Equal(g.links)
	g.title, g.links, g.notes = i.Title, i.Links, i.Notes
	g.playStatus, g.rating = i.PlayStatus, i.Rating
	g.fields = i.Fields.compact()
	g.updatedAt = now

	return linksChanged, nil
}

// AddCopy adds a manual copy.
func (g *Game) AddCopy(d CopyDetails, now time.Time) (Copy, error) {
	return g.addCopy(d, copyOrigin{}, now)
}

// copyOrigin is where an imported copy comes from: its source, its id there, and the system the
// source says it is played on. A manual copy has none.
type copyOrigin struct {
	sourceID     string
	externalID   string
	sourceSystem string
}

func (g *Game) addCopy(d CopyDetails, from copyOrigin, now time.Time) (Copy, error) {
	d, err := d.normalize()
	if err != nil {
		return Copy{}, err
	}

	c := Copy{
		ID:           NewID(),
		CopyDetails:  d,
		SourceID:     from.sourceID,
		ExternalID:   from.externalID,
		SourceSystem: from.sourceSystem, // before reconciling: it decides the copy's system
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	g.copies = append(g.copies, c)
	g.reconcileEditions() // the first copy takes the cover chosen for the game
	g.updatedAt = now

	return c, nil
}

// UpdateCopy replaces the details of a copy.
func (g *Game) UpdateCopy(id ID, d CopyDetails, now time.Time) (Copy, error) {
	i := g.indexOf(id)
	if i < 0 {
		return Copy{}, ErrCopyNotFound
	}

	d, err := d.normalize()
	if err != nil {
		return Copy{}, err
	}

	if d.Kind != KindPhysical || d.Barcode != g.copies[i].Barcode {
		g.copies[i].clearValuation() // estimates belong to the product the barcode names
	}

	g.copies[i].CopyDetails = d
	g.reconcileEditions() // an override can move the copy to another system
	g.copies[i].UpdatedAt = now
	g.updatedAt = now

	return g.copies[i].clone(), nil
}

// RemoveCopy detaches a copy from the game and returns it.
func (g *Game) RemoveCopy(id ID, now time.Time) (Copy, error) {
	i := g.indexOf(id)
	if i < 0 {
		return Copy{}, ErrCopyNotFound
	}

	c := g.copies[i]
	g.copies = append(g.copies[:i], g.copies[i+1:]...)
	g.reconcileEditions()
	g.updatedAt = now

	return c, nil
}

// AttachCopy adopts a copy removed from another game (used to move or merge copies).
func (g *Game) AttachCopy(c Copy, now time.Time) {
	g.attachCopy(c, now)
	g.reconcileEditions() // the first copy takes the cover chosen for the game
}

func (g *Game) attachCopy(c Copy, now time.Time) {
	c.UpdatedAt = now
	g.copies = append(g.copies, c)
	g.updatedAt = now
}

// Absorb moves every copy of other into g. The caller must delete other afterwards.
func (g *Game) Absorb(other *Game, now time.Time) {
	// Editions are reconciled once every copy is in, so the main edition is the final one.
	for _, c := range other.copies {
		g.attachCopy(c, now)
	}

	other.copies = nil
	g.links.fill(other.links)

	// Covers chosen for this game win; the other's fill the systems this game chose none for.
	for system, c := range other.covers {
		if _, ok := g.covers[system]; ok {
			continue
		}

		if g.covers == nil {
			g.covers = map[string]EditionCover{}
		}

		g.covers[system] = c
	}

	if g.mainSystem == "" {
		g.mainSystem = other.mainSystem
	}

	// The play status and the rating describe the same game, so the other's only fill gaps.
	if g.playStatus == PlayNone {
		g.playStatus = other.playStatus
	}

	if g.rating == 0 {
		g.rating = other.rating
	}

	g.fields = fillFields(g.fields, other.fields)

	if other.notes != "" && !strings.Contains(g.notes, other.notes) {
		g.notes = strings.TrimSpace(g.notes + "\n" + other.notes)
	}

	g.reconcileEditions()
	g.updatedAt = now
}

// RemoveCopiesFromSource drops every copy managed by sourceID and returns how many were removed.
func (g *Game) RemoveCopiesFromSource(sourceID string, now time.Time) int {
	kept := g.copies[:0]
	for _, c := range g.copies {
		if c.SourceID != sourceID {
			kept = append(kept, c)
		}
	}

	n := len(g.copies) - len(kept)

	g.copies = kept
	g.reconcileEditions()

	if n > 0 {
		g.updatedAt = now
	}

	return n
}

// ReleaseCopiesFromSource turns copies managed by sourceID into manual copies.
func (g *Game) ReleaseCopiesFromSource(sourceID string, now time.Time) int {
	n := 0

	for i := range g.copies {
		if g.copies[i].SourceID == sourceID {
			g.copies[i].SourceID = ""
			g.copies[i].UpdatedAt = now
			n++
		}
	}

	if n > 0 {
		g.updatedAt = now
	}

	return n
}

// CopyWithBarcode returns the copy carrying barcode b, if any.
func (g *Game) CopyWithBarcode(b Barcode) (Copy, bool) {
	for _, c := range g.copies {
		if b != "" && c.Barcode == b {
			return c, true
		}
	}

	return Copy{}, false
}

// IsRedundant reports whether c is a pending key for a game you already have in a library
// on the same platform: you do not need it and could gift it.
func (g *Game) IsRedundant(c Copy) bool {
	if !c.IsPendingKey() {
		return false
	}

	for _, o := range g.copies {
		if o.Kind == KindLibrary && strings.EqualFold(o.Platform, c.Platform) {
			return true
		}
	}

	return false
}

// MarkRedundantKeysRedeemed marks revealed keys as redeemed when the game is already in the
// library of the same platform (stores never tell whether a key was redeemed).
func (g *Game) MarkRedundantKeysRedeemed(now time.Time) int {
	n := 0

	for i, c := range g.copies {
		if c.Status == StatusRevealed && g.IsRedundant(c) {
			g.copies[i].Status = StatusRedeemed
			g.copies[i].UpdatedAt = now
			n++
		}
	}

	if n > 0 {
		g.updatedAt = now
	}

	return n
}

func (g *Game) indexOf(id ID) int {
	for i := range g.copies {
		if g.copies[i].ID == id {
			return i
		}
	}

	return -1
}

// Repository is the persistence port for games. Save stores the whole aggregate, copies included.
type Repository interface {
	// List returns every game with its copies.
	List(ctx context.Context) ([]*Game, error)

	// Get returns one game with its copies, or ErrGameNotFound.
	Get(ctx context.Context, id ID) (*Game, error)

	// Save creates or updates a game and replaces its copies.
	Save(ctx context.Context, g *Game) error

	// Delete removes a game and its copies.
	Delete(ctx context.Context, id ID) error
}
