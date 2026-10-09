package rpc

import (
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"gamevault/internal/application/catalog"
	"gamevault/internal/application/media"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
	"gamevault/internal/domain/source"
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

var (
	kindFromPB    = invert(kindToPB)
	statusFromPB  = invert(statusToPB)
	gradeFromPB   = invert(gradeToPB)
	contentFromPB = invert(contentToPB)
)

func invert[K, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}

	return out
}

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
		Id:           string(g.ID()),
		Title:        g.Title(),
		Links:        g.Links(),
		Notes:        g.Notes(),
		CoverUrl:     g.CoverURL(),
		CoverPhotoId: string(g.CoverPhoto()),
		CreatedAt:    ts(g.CreatedAt()),
		UpdatedAt:    ts(g.UpdatedAt()),
	}
	for _, c := range g.Copies() {
		out.Copies = append(out.Copies, &pb.Copy{
			Id:            string(c.ID),
			Details:       detailsToPB(c.CopyDetails),
			SourceId:      c.SourceID,
			ExternalId:    c.ExternalID,
			Redundant:     g.IsRedundant(c),
			Photos:        photosToPB(c.Photos),
			Estimates:     estimatesToPB(c.Estimates),
			NextValuation: optionalTS(c.NextValuation),
			ValuedAt:      optionalTS(c.ValuedAt),
			CreatedAt:     ts(c.CreatedAt),
			UpdatedAt:     ts(c.UpdatedAt),
		})
	}

	return out
}

func photosToPB(photos []game.Photo) []*pb.Photo {
	out := make([]*pb.Photo, 0, len(photos))
	for _, p := range photos {
		ph := &pb.Photo{
			Id:      string(p.ID),
			Caption: p.Caption,
			AddedAt: ts(p.AddedAt),
		}
		if !p.TakenAt.IsZero() {
			ph.TakenAt = ts(p.TakenAt)
		}

		out = append(out, ph)
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

	return game.CopyDetails{
		Kind:       kindFromPB[d.Kind],
		Platform:   d.Platform,
		Status:     statusFromPB[d.Status],
		Key:        d.Key,
		RedeemBy:   redeemBy,
		Origin:     d.Origin,
		AcquiredOn: acquired,
		Edition:    d.Edition,
		Grade:      gradeFromPB[d.Grade],
		Contents:   contents,
		Location:   d.Location,
		Price: game.Money{
			Amount:   d.GetPrice().GetAmountMinor(),
			Currency: d.GetPrice().GetCurrency(),
		},
		Notes:   d.Notes,
		Barcode: barcode,
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
		if c, ok := contentFromPB[x]; ok {
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

func optionalTS(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return ts(t)
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

func reportToPB(r *source.SyncReport) *pb.SyncReport {
	if r == nil {
		return nil
	}

	return &pb.SyncReport{
		StartedAt:       ts(r.StartedAt),
		FinishedAt:      ts(r.FinishedAt),
		Success:         r.Success(),
		Error:           r.Err,
		Fetched:         int32(r.Fetched),
		CopiesAdded:     int32(r.CopiesAdded),
		CopiesUpdated:   int32(r.CopiesUpdated),
		CopiesUnchanged: int32(r.CopiesUnchanged),
		GamesCreated:    int32(r.GamesCreated),
		Warnings:        r.Warnings,
	}
}

func fieldsToPB(fields schema.Fields) []*pb.SettingField {
	public := fields.Public()
	out := make([]*pb.SettingField, 0, len(public))

	for _, f := range public {
		kind := pb.SettingField_KIND_TEXT

		switch f.Kind {
		case schema.FieldSecret:
			kind = pb.SettingField_KIND_SECRET
		case schema.FieldConsent:
			kind = pb.SettingField_KIND_CONSENT
		}

		var signIn string

		if f.SignIn != nil {
			if b, err := json.Marshal(f.SignIn.WithOpen(f.HelpURL)); err == nil {
				signIn = string(b)
			}
		}

		out = append(out, &pb.SettingField{
			SignIn:   signIn,
			Key:      f.Key,
			LabelKey: f.LabelKey,
			Kind:     kind,
			Required: f.Required,
			HelpKey:  f.HelpKey,
			HelpUrl:  f.HelpURL,
		})
	}

	return out
}

func descriptorToPB(d source.TypeDescriptor) *pb.SourceType {
	return &pb.SourceType{
		Id:             string(d.Type),
		Name:           d.Name,
		DescriptionKey: d.DescriptionKey,
		Fields:         fieldsToPB(d.Fields),
		ManualScans:    d.ManualScans,
	}
}

func providerToPB(v media.ProviderView) *pb.Provider {
	return &pb.Provider{
		Id:             string(v.ID()),
		Kind:           string(v.Kind()),
		Name:           v.Descriptor.Name,
		DescriptionKey: v.Descriptor.DescriptionKey,
		Fields:         fieldsToPB(v.Descriptor.Fields),
		Enabled:        v.Enabled(),
		Priority:       int32(v.Priority()),
		Settings:       v.Descriptor.Fields.Masked(v.Settings()),
	}
}

// sourceToPB maps a source, masking secrets with the type descriptor.
func sourceToPB(v sync.SourceView, d source.TypeDescriptor) *pb.Source {
	return &pb.Source{
		Id:                string(v.ID()),
		Type:              string(v.Type()),
		Name:              v.Name(),
		Enabled:           v.Enabled(),
		SyncIntervalHours: uint32(v.SyncInterval() / time.Hour),
		Settings:          d.Masked(v.Settings()),
		LastSync:          reportToPB(v.LastSync()),
		CopyCount:         int32(v.CopyCount),
		CreatedAt:         ts(v.CreatedAt()),
		UpdatedAt:         ts(v.UpdatedAt()),
	}
}

func configFromPB(in *pb.SourceInput) source.Config {
	if in == nil {
		return source.Config{}
	}

	return source.Config{
		Name:         in.Name,
		Enabled:      in.Enabled,
		SyncInterval: time.Duration(in.SyncIntervalHours) * time.Hour,
		Settings:     source.Settings(in.Settings),
	}
}

// toConnectError maps domain and application errors to Connect status codes.
func toConnectError(err error) error {
	if err == nil {
		return nil
	}

	var (
		ce   *connect.Error
		gv   *game.ValidationError
		sv   *source.ValidationError
		setv *settings.ValidationError
		schv *schema.ValidationError
	)
	switch {
	case errors.As(err, &ce):
		return err
	case errors.Is(err, game.ErrGameNotFound), errors.Is(err, game.ErrCopyNotFound), errors.Is(err, game.ErrPhotoNotFound), errors.Is(err, source.ErrNotFound),
		errors.Is(err, provider.ErrNotFound), errors.Is(err, media.ErrUnknownProvider):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.As(err, &gv), errors.As(err, &sv), errors.As(err, &setv), errors.As(err, &schv), errors.Is(err, source.ErrUnknownType),
		errors.Is(err, game.ErrInvalidBarcode):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, catalog.ErrPhotoNotUploaded), errors.Is(err, valuation.ErrNotValuable), errors.Is(err, valuation.ErrNoProviders):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, valuation.ErrChanged):
		return connect.NewError(connect.CodeAborted, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
