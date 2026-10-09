package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.MetadataServiceHandler = (*MediaHandler)(nil)

// GetGameDetails returns the details of a game from the metadata provider chain.
func (h *MediaHandler) GetGameDetails(ctx context.Context, req *connect.Request[pb.GetGameDetailsRequest]) (*connect.Response[pb.GetGameDetailsResponse], error) {
	d, warnings, err := h.media.GameDetails(ctx, game.ID(req.Msg.GameId), req.Msg.Language, req.Msg.Refresh)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.GameDetails{
		Summary: d.Summary, Genres: d.Genres, Developers: d.Developers, Publishers: d.Publishers,
		ReleaseDate: d.ReleaseDate, AgeRating: d.AgeRating, Players: d.Players, Metacritic: int32(d.Metacritic),
		MetacriticUrl: d.MetacriticURL, Website: d.Website, StoreUrl: d.StoreURL, Language: d.Language, FetchedAt: ts(d.FetchedAt),
	}
	// Images are served from the game's folder; the remote originals are only used to fill it.
	id := game.ID(req.Msg.GameId)

	local := func(name, remote string) string {
		if name == "" {
			return remote
		}

		return gameAssetPath(id, name)
	}
	for _, v := range d.Videos {
		out.Videos = append(out.Videos, &pb.Video{Title: v.Title, ThumbnailUrl: local(v.ThumbAsset, v.Thumbnail), HlsUrl: v.HLSURL, YoutubeId: v.YouTubeID})
	}

	for _, s := range d.Screenshots {
		out.Screenshots = append(out.Screenshots, &pb.Screenshot{ThumbUrl: local(s.ThumbAsset, s.ThumbURL), FullUrl: local(s.FullAsset, s.FullURL)})
	}

	for _, s := range d.Sources {
		out.Sources = append(out.Sources, h.media.ProviderName(s))
	}

	return connect.NewResponse(&pb.GetGameDetailsResponse{Details: out, Warnings: warnings}), nil
}
