package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/catalog"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// GameHandler implements gamevaultv1connect.GameServiceHandler on top of the catalog and media use cases.
type GameHandler struct {
	catalog *catalog.Service
	media   *media.Service
}

var _ gamevaultv1connect.GameServiceHandler = (*GameHandler)(nil)

func NewGameHandler(c *catalog.Service, m *media.Service) *GameHandler {
	return &GameHandler{catalog: c, media: m}
}

func gameResp[T any](g *game.Game, err error, wrap func(*pb.Game) *T) (*connect.Response[T], error) {
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(wrap(gameToPB(g))), nil
}

func (h *GameHandler) ListGames(ctx context.Context, req *connect.Request[pb.ListGamesRequest]) (*connect.Response[pb.ListGamesResponse], error) {
	games, err := h.catalog.ListGames(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	summaries, err := h.media.CatalogSummaries(ctx, req.Msg.Language)
	if err != nil {
		return nil, toConnectError(err)
	}
	out := make([]*pb.Game, len(games))
	cached := 0
	for i, g := range games {
		out[i] = gameToPB(g)
		if s, ok := summaries[g.ID()]; ok {
			out[i].Genres, out[i].ReleaseYear = s.Genres, int32(s.Year())
			cached++
		}
	}
	return connect.NewResponse(&pb.ListGamesResponse{Games: out, DetailsCached: int32(cached)}), nil
}

func (h *GameHandler) GetGame(ctx context.Context, req *connect.Request[pb.GetGameRequest]) (*connect.Response[pb.GetGameResponse], error) {
	g, err := h.catalog.GetGame(ctx, game.ID(req.Msg.Id))
	return gameResp(g, err, func(g *pb.Game) *pb.GetGameResponse { return &pb.GetGameResponse{Game: g} })
}

func (h *GameHandler) CreateGame(ctx context.Context, req *connect.Request[pb.CreateGameRequest]) (*connect.Response[pb.CreateGameResponse], error) {
	var copies []game.CopyDetails
	for _, d := range req.Msg.Copies {
		cd, err := detailsFromPB(d)
		if err != nil {
			return nil, toConnectError(err)
		}
		copies = append(copies, cd)
	}
	info := game.Info{Title: req.Msg.Title, SteamAppID: req.Msg.SteamAppId, Notes: req.Msg.Notes, CoverURL: req.Msg.CoverUrl}
	g, err := h.catalog.CreateGame(ctx, info, copies)
	return gameResp(g, err, func(g *pb.Game) *pb.CreateGameResponse { return &pb.CreateGameResponse{Game: g} })
}

func (h *GameHandler) UpdateGame(ctx context.Context, req *connect.Request[pb.UpdateGameRequest]) (*connect.Response[pb.UpdateGameResponse], error) {
	info := game.Info{Title: req.Msg.Title, SteamAppID: req.Msg.SteamAppId, Notes: req.Msg.Notes, CoverURL: req.Msg.CoverUrl}
	g, err := h.catalog.UpdateGame(ctx, game.ID(req.Msg.Id), info)
	return gameResp(g, err, func(g *pb.Game) *pb.UpdateGameResponse { return &pb.UpdateGameResponse{Game: g} })
}

func (h *GameHandler) DeleteGame(ctx context.Context, req *connect.Request[pb.DeleteGameRequest]) (*connect.Response[pb.DeleteGameResponse], error) {
	if err := h.catalog.DeleteGame(ctx, game.ID(req.Msg.Id)); err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.DeleteGameResponse{}), nil
}

func (h *GameHandler) MergeGames(ctx context.Context, req *connect.Request[pb.MergeGamesRequest]) (*connect.Response[pb.MergeGamesResponse], error) {
	ids := make([]game.ID, len(req.Msg.SourceIds))
	for i, id := range req.Msg.SourceIds {
		ids[i] = game.ID(id)
	}
	g, err := h.catalog.MergeGames(ctx, game.ID(req.Msg.TargetId), ids)
	return gameResp(g, err, func(g *pb.Game) *pb.MergeGamesResponse { return &pb.MergeGamesResponse{Game: g} })
}

func (h *GameHandler) AddCopy(ctx context.Context, req *connect.Request[pb.AddCopyRequest]) (*connect.Response[pb.AddCopyResponse], error) {
	d, err := detailsFromPB(req.Msg.Details)
	if err != nil {
		return nil, toConnectError(err)
	}
	g, err := h.catalog.AddCopy(ctx, game.ID(req.Msg.GameId), d)
	return gameResp(g, err, func(g *pb.Game) *pb.AddCopyResponse { return &pb.AddCopyResponse{Game: g} })
}

func (h *GameHandler) UpdateCopy(ctx context.Context, req *connect.Request[pb.UpdateCopyRequest]) (*connect.Response[pb.UpdateCopyResponse], error) {
	d, err := detailsFromPB(req.Msg.Details)
	if err != nil {
		return nil, toConnectError(err)
	}
	g, err := h.catalog.UpdateCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId), d)
	return gameResp(g, err, func(g *pb.Game) *pb.UpdateCopyResponse { return &pb.UpdateCopyResponse{Game: g} })
}

func (h *GameHandler) DeleteCopy(ctx context.Context, req *connect.Request[pb.DeleteCopyRequest]) (*connect.Response[pb.DeleteCopyResponse], error) {
	g, err := h.catalog.DeleteCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId))
	return gameResp(g, err, func(g *pb.Game) *pb.DeleteCopyResponse { return &pb.DeleteCopyResponse{Game: g} })
}

func (h *GameHandler) MoveCopy(ctx context.Context, req *connect.Request[pb.MoveCopyRequest]) (*connect.Response[pb.MoveCopyResponse], error) {
	src, dst, err := h.catalog.MoveCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId), game.ID(req.Msg.TargetGameId), req.Msg.NewGameTitle)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.MoveCopyResponse{SourceGame: gameToPB(src), TargetGame: gameToPB(dst)}), nil
}

func (h *GameHandler) MarkRedeemedKeys(ctx context.Context, _ *connect.Request[pb.MarkRedeemedKeysRequest]) (*connect.Response[pb.MarkRedeemedKeysResponse], error) {
	n, err := h.catalog.MarkRedeemedKeys(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}
	return connect.NewResponse(&pb.MarkRedeemedKeysResponse{Updated: int32(n)}), nil
}

func (h *GameHandler) SearchSteamApps(ctx context.Context, req *connect.Request[pb.SearchSteamAppsRequest]) (*connect.Response[pb.SearchSteamAppsResponse], error) {
	matches, err := h.media.SearchSteamApps(ctx, req.Msg.Query)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	out := &pb.SearchSteamAppsResponse{}
	for _, m := range matches {
		out.Apps = append(out.Apps, &pb.SteamApp{AppId: m.AppID, Name: m.Name, ImageUrl: m.ImageURL})
	}
	return connect.NewResponse(out), nil
}
