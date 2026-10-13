package rpc

import (
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
)

var kindToPB = map[game.Kind]pb.CopyKind{
	game.KindKey:      pb.CopyKind_COPY_KIND_KEY,
	game.KindLibrary:  pb.CopyKind_COPY_KIND_LIBRARY,
	game.KindPhysical: pb.CopyKind_COPY_KIND_PHYSICAL,
}

var statusToPB = map[game.Status]pb.CopyStatus{
	game.StatusUnrevealed: pb.CopyStatus_COPY_STATUS_UNREVEALED,
	game.StatusRevealed:   pb.CopyStatus_COPY_STATUS_REVEALED,
	game.StatusRedeemed:   pb.CopyStatus_COPY_STATUS_REDEEMED,
	game.StatusGifted:     pb.CopyStatus_COPY_STATUS_GIFTED,
	game.StatusExpired:    pb.CopyStatus_COPY_STATUS_EXPIRED,
	game.StatusOwned:      pb.CopyStatus_COPY_STATUS_OWNED,
	game.StatusLent:       pb.CopyStatus_COPY_STATUS_LENT,
	game.StatusSold:       pb.CopyStatus_COPY_STATUS_SOLD,
}

var gradeToPB = map[game.Grade]pb.CopyGrade{
	game.GradeSealed:     pb.CopyGrade_COPY_GRADE_SEALED,
	game.GradeMint:       pb.CopyGrade_COPY_GRADE_MINT,
	game.GradeVeryGood:   pb.CopyGrade_COPY_GRADE_VERY_GOOD,
	game.GradeGood:       pb.CopyGrade_COPY_GRADE_GOOD,
	game.GradeAcceptable: pb.CopyGrade_COPY_GRADE_ACCEPTABLE,
	game.GradeDamaged:    pb.CopyGrade_COPY_GRADE_DAMAGED,
}

var contentToPB = map[game.Content]pb.CopyContent{
	game.ContentBox:    pb.CopyContent_COPY_CONTENT_BOX,
	game.ContentManual: pb.CopyContent_COPY_CONTENT_MANUAL,
	game.ContentMedia:  pb.CopyContent_COPY_CONTENT_MEDIA,
	game.ContentExtras: pb.CopyContent_COPY_CONTENT_EXTRAS,
}

var playStatusToPB = map[game.PlayStatus]pb.PlayStatus{
	game.PlayNone:      pb.PlayStatus_PLAY_STATUS_UNSPECIFIED,
	game.PlayBacklog:   pb.PlayStatus_PLAY_STATUS_BACKLOG,
	game.PlayPlaying:   pb.PlayStatus_PLAY_STATUS_PLAYING,
	game.PlayFinished:  pb.PlayStatus_PLAY_STATUS_FINISHED,
	game.PlayAbandoned: pb.PlayStatus_PLAY_STATUS_ABANDONED,
}

var (
	playStatusFromPB = invert(playStatusToPB)
	kindFromPB       = invert(kindToPB)
	statusFromPB     = invert(statusToPB)
	gradeFromPB      = invert(gradeToPB)
	contentFromPB    = invert(contentToPB)
)

// enumFromPB reads a closed value from the API: the unspecified value (the enum's zero) is the
// domain's zero value, for the domain to fill in, and a value this server does not know is refused.
// An old tab left open across an upgrade sends such values; refusing them asks for a reload
// instead of quietly saving a default. what names the value in the error ("copy status").
func enumFromPB[K any, V comparable](m map[V]K, v V, what string) (K, error) {
	if k, ok := m[v]; ok {
		return k, nil
	}

	var (
		none        K
		unspecified V
	)

	if v == unspecified {
		return none, nil
	}

	return none, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("the %s is not valid: reload the page and try again", what))
}

func invert[K, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}

	return out
}

// ts maps a time onto the API; the zero time (never set) is no timestamp at all.
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return timestamppb.New(t)
}

func gameToPB(g *game.Game) *pb.Game {
	if g == nil {
		return nil
	}

	out := &pb.Game{
		Id:             string(g.ID()),
		Title:          g.Title(),
		Links:          g.Links(),
		Notes:          g.Notes(),
		PlayStatus:     playStatusToPB[g.PlayStatus()],
		Rating:         int32(g.Rating()),
		CreatedAt:      ts(g.CreatedAt()),
		UpdatedAt:      ts(g.UpdatedAt()),
		Fields:         fieldValuesToPB(g.Fields()),
		Editions:       editionsToPB(g),
		MainSystem:     g.MainSystem(),
		HasPcStoreLink: g.Links().HasPCStore(),
		AutoLinks:      g.AutoLinks(),
	}
	for _, c := range g.Copies() {
		details := detailsToPB(c.CopyDetails)
		details.Fields = fieldValuesToPB(c.Fields)

		out.Copies = append(out.Copies, &pb.Copy{
			Id:              string(c.ID),
			Details:         details,
			SourceId:        c.SourceID,
			ExternalId:      c.ExternalID,
			Redundant:       g.IsRedundant(c),
			Photos:          photosToPB(c.Photos),
			Estimates:       estimatesToPB(c.Estimates),
			NextValuation:   ts(c.NextValuation),
			ValuedAt:        ts(c.ValuedAt),
			EffectiveSystem: c.System(),
			CreatedAt:       ts(c.CreatedAt),
			UpdatedAt:       ts(c.UpdatedAt),
		})
	}

	return out
}

// editionsToPB lists the game's editions. A game without copies has none, so it gets its main
// edition on the empty system: that is where the cover chosen for the game lives until its first
// copy.
func editionsToPB(g *game.Game) []*pb.Edition {
	editions := g.Editions()
	if len(editions) == 0 {
		main := g.MainEdition()
		main.Main = true
		editions = []game.Edition{main}
	}

	out := make([]*pb.Edition, 0, len(editions))

	for _, e := range editions {
		out = append(out, &pb.Edition{
			System:       e.System,
			CoverUrl:     e.Cover.URL,
			CoverPhotoId: string(e.Cover.Photo),
			Main:         e.Main,
		})
	}

	return out
}

func photosToPB(photos []game.Photo) []*pb.Photo {
	out := make([]*pb.Photo, 0, len(photos))
	for _, p := range photos {
		out = append(out, &pb.Photo{
			Id:      string(p.ID),
			Caption: p.Caption,
			AddedAt: ts(p.AddedAt),
			TakenAt: ts(p.TakenAt),
		})
	}

	return out
}

func detailsToPB(d game.CopyDetails) *pb.CopyDetails {
	return &pb.CopyDetails{
		Kind:       kindToPB[d.Kind],
		Platform:   d.Platform,
		Status:     statusToPB[d.Status],
		Key:        d.Key,
		RedeemBy:   string(d.RedeemBy),
		Origin:     d.Origin,
		AcquiredOn: string(d.AcquiredOn),
		Edition:    d.Edition,
		Grade:      gradeToPB[d.Grade],
		Contents:   contentsToPB(d.Contents),
		Location:   d.Location,
		Price:      moneyToPB(d.Price),
		Notes:      d.Notes,
		Barcode:    string(d.Barcode),
		System:     d.System,
	}
}

func detailsFromPB(d *pb.CopyDetails) (game.CopyDetails, error) {
	if d == nil {
		return game.CopyDetails{}, connect.NewError(connect.CodeInvalidArgument, errors.New("copy details are required"))
	}

	redeemBy, err := game.ParseDate(d.RedeemBy)
	if err != nil {
		return game.CopyDetails{}, err
	}

	acquired, err := game.ParseDate(d.AcquiredOn)
	if err != nil {
		return game.CopyDetails{}, err
	}

	barcode, err := game.ParseBarcode(d.Barcode)
	if err != nil {
		return game.CopyDetails{}, err
	}

	contents, err := contentsFromPB(d.Contents)
	if err != nil {
		return game.CopyDetails{}, err
	}

	kind, err := enumFromPB(kindFromPB, d.Kind, "copy kind")
	if err != nil {
		return game.CopyDetails{}, err
	}

	status, err := enumFromPB(statusFromPB, d.Status, "copy status")
	if err != nil {
		return game.CopyDetails{}, err
	}

	grade, err := enumFromPB(gradeFromPB, d.Grade, "copy grade")
	if err != nil {
		return game.CopyDetails{}, err
	}

	return game.CopyDetails{
		Kind:       kind,
		Platform:   d.Platform,
		Status:     status,
		Key:        d.Key,
		RedeemBy:   redeemBy,
		Origin:     d.Origin,
		AcquiredOn: acquired,
		Edition:    d.Edition,
		Grade:      grade,
		Contents:   contents,
		Location:   d.Location,
		Price: game.Money{
			Amount:   d.GetPrice().GetAmountMinor(),
			Currency: d.GetPrice().GetCurrency(),
		},
		Notes:   d.Notes,
		Barcode: barcode,
		System:  d.System,
	}, nil
}

func contentsToPB(c game.Contents) []pb.CopyContent {
	list := c.List()
	out := make([]pb.CopyContent, 0, len(list))

	for _, x := range list {
		out = append(out, contentToPB[x])
	}

	return out
}

func contentsFromPB(in []pb.CopyContent) (game.Contents, error) {
	parts := make([]game.Content, 0, len(in))
	for _, x := range in {
		c, err := enumFromPB(contentFromPB, x, "copy content")
		if err != nil {
			return 0, err
		}

		if c != "" { // unspecified: nothing to add
			parts = append(parts, c)
		}
	}

	return game.ContentsOf(parts...)
}

func moneyToPB(m game.Money) *pb.Money {
	if m.IsZero() {
		return nil
	}

	return &pb.Money{
		AmountMinor: m.Amount,
		Currency:    m.Currency,
	}
}

func estimatesToPB(estimates []game.Estimate) []*pb.Estimate {
	out := make([]*pb.Estimate, 0, len(estimates))
	for _, e := range estimates {
		out = append(out, &pb.Estimate{
			Provider:  e.Provider,
			Sell:      moneyToPB(e.Sell),
			BuyCash:   moneyToPB(e.BuyCash),
			BuyCredit: moneyToPB(e.BuyCredit),
			Listings:  int32(e.Listings),
			Url:       e.URL,
			FetchedAt: ts(e.FetchedAt),
		})
	}

	return out
}
