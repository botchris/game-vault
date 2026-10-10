package game

import "slices"

// PlayStatus says where the user is with a game: whether they mean to play it, are playing it or
// are done with it. It belongs to the game, not to a copy: owning it twice does not mean playing
// it twice.
type PlayStatus string

// Values of PlayStatus. The empty value means the user has not said.
const (
	PlayNone      PlayStatus = ""
	PlayBacklog   PlayStatus = "backlog"   // owned and meant to be played
	PlayPlaying   PlayStatus = "playing"   // being played now
	PlayFinished  PlayStatus = "finished"  // played to the end, or as far as the user wanted
	PlayAbandoned PlayStatus = "abandoned" // started and dropped
)

var playStatuses = []PlayStatus{PlayNone, PlayBacklog, PlayPlaying, PlayFinished, PlayAbandoned}

// Valid reports whether s is a known play status, the empty one included.
func (s PlayStatus) Valid() bool { return slices.Contains(playStatuses, s) }

// MaxRating is the highest rating a game can get. Zero means unrated.
const MaxRating = 5

// Rating is the user's score for a game, from 1 to MaxRating stars; 0 means unrated.
type Rating int

// Valid reports whether r is unrated or within 1..MaxRating.
func (r Rating) Valid() bool { return r >= 0 && r <= MaxRating }
