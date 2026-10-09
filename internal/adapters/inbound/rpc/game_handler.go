package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"gamevault/internal/application/catalog"
	"gamevault/internal/application/media"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// GameHandler implements gamevaultv1connect.GameServiceHandler on top of the catalog and media use
// cases; the sources only tell which stores they link games to.
type GameHandler struct {
	catalog *catalog.Service
	media   *media.Service
	sources *sync.Service
}

var _ gamevaultv1connect.GameServiceHandler = (*GameHandler)(nil)

// NewGameHandler returns the GameService handler backed by the catalog, media and sync services.
func NewGameHandler(c *catalog.Service, m *media.Service, sources *sync.Service) *GameHandler {
	return &GameHandler{
		catalog: c,
		media:   m,
		sources: sources,
	}
}

func gameResp[T any](g *game.Game, err error, wrap func(*pb.Game) *T) (*connect.Response[T], error) {
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(wrap(gameToPB(g))), nil
}

// ListGames returns the catalog, filtered and sorted as requested.
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

	return connect.NewResponse(&pb.ListGamesResponse{
		Games:         out,
		DetailsCached: int32(cached),
	}), nil
}

// GetGame returns one game with its copies.
func (h *GameHandler) GetGame(ctx context.Context, req *connect.Request[pb.GetGameRequest]) (*connect.Response[pb.GetGameResponse], error) {
	g, err := h.catalog.GetGame(ctx, game.ID(req.Msg.Id))
	return gameResp(g, err, func(g *pb.Game) *pb.GetGameResponse { return &pb.GetGameResponse{Game: g} })
}

// CreateGame adds a game to the catalog, consolidating it with an existing one when they match.
func (h *GameHandler) CreateGame(ctx context.Context, req *connect.Request[pb.CreateGameRequest]) (*connect.Response[pb.CreateGameResponse], error) {
	var copies []game.CopyDetails

	for _, d := range req.Msg.Copies {
		cd, err := detailsFromPB(d)
		if err != nil {
			return nil, toConnectError(err)
		}

		copies = append(copies, cd)
	}

	info := game.Info{
		Title:    req.Msg.Title,
		Links:    req.Msg.Links,
		Notes:    req.Msg.Notes,
		CoverURL: req.Msg.CoverUrl,
	}
	g, err := h.catalog.CreateGame(ctx, info, copies)

	return gameResp(g, err, func(g *pb.Game) *pb.CreateGameResponse { return &pb.CreateGameResponse{Game: g} })
}

// UpdateGame changes the editable fields of a game.
func (h *GameHandler) UpdateGame(ctx context.Context, req *connect.Request[pb.UpdateGameRequest]) (*connect.Response[pb.UpdateGameResponse], error) {
	info := game.Info{
		Title:    req.Msg.Title,
		Links:    req.Msg.Links,
		Notes:    req.Msg.Notes,
		CoverURL: req.Msg.CoverUrl,
	}
	g, err := h.catalog.UpdateGame(ctx, game.ID(req.Msg.Id), info)

	return gameResp(g, err, func(g *pb.Game) *pb.UpdateGameResponse { return &pb.UpdateGameResponse{Game: g} })
}

// DeleteGame removes a game and all its copies.
func (h *GameHandler) DeleteGame(ctx context.Context, req *connect.Request[pb.DeleteGameRequest]) (*connect.Response[pb.DeleteGameResponse], error) {
	if err := h.catalog.DeleteGame(ctx, game.ID(req.Msg.Id)); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.DeleteGameResponse{}), nil
}

// MergeGames moves the copies of several games into one and removes the others.
func (h *GameHandler) MergeGames(ctx context.Context, req *connect.Request[pb.MergeGamesRequest]) (*connect.Response[pb.MergeGamesResponse], error) {
	ids := make([]game.ID, len(req.Msg.SourceIds))
	for i, id := range req.Msg.SourceIds {
		ids[i] = game.ID(id)
	}

	g, err := h.catalog.MergeGames(ctx, game.ID(req.Msg.TargetId), ids)

	return gameResp(g, err, func(g *pb.Game) *pb.MergeGamesResponse { return &pb.MergeGamesResponse{Game: g} })
}

// AddCopy attaches a new copy to a game.
func (h *GameHandler) AddCopy(ctx context.Context, req *connect.Request[pb.AddCopyRequest]) (*connect.Response[pb.AddCopyResponse], error) {
	d, err := detailsFromPB(req.Msg.Details)
	if err != nil {
		return nil, toConnectError(err)
	}

	g, err := h.catalog.AddCopy(ctx, game.ID(req.Msg.GameId), d)

	return gameResp(g, err, func(g *pb.Game) *pb.AddCopyResponse { return &pb.AddCopyResponse{Game: g} })
}

// UpdateCopy changes the details of a copy.
func (h *GameHandler) UpdateCopy(ctx context.Context, req *connect.Request[pb.UpdateCopyRequest]) (*connect.Response[pb.UpdateCopyResponse], error) {
	d, err := detailsFromPB(req.Msg.Details)
	if err != nil {
		return nil, toConnectError(err)
	}

	g, err := h.catalog.UpdateCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId), d)

	return gameResp(g, err, func(g *pb.Game) *pb.UpdateCopyResponse { return &pb.UpdateCopyResponse{Game: g} })
}

// DeleteCopy removes a copy from its game.
func (h *GameHandler) DeleteCopy(ctx context.Context, req *connect.Request[pb.DeleteCopyRequest]) (*connect.Response[pb.DeleteCopyResponse], error) {
	g, err := h.catalog.DeleteCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId))
	return gameResp(g, err, func(g *pb.Game) *pb.DeleteCopyResponse { return &pb.DeleteCopyResponse{Game: g} })
}

// MoveCopy reassigns a copy to another game.
func (h *GameHandler) MoveCopy(ctx context.Context, req *connect.Request[pb.MoveCopyRequest]) (*connect.Response[pb.MoveCopyResponse], error) {
	src, dst, err := h.catalog.MoveCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId), game.ID(req.Msg.TargetGameId), req.Msg.NewGameTitle)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.MoveCopyResponse{
		SourceGame: gameToPB(src),
		TargetGame: gameToPB(dst),
	}), nil
}

// MarkRedeemedKeys marks as redeemed the unredeemed keys whose game is already in the
// library of the key's platform.
func (h *GameHandler) MarkRedeemedKeys(ctx context.Context, _ *connect.Request[pb.MarkRedeemedKeysRequest]) (*connect.Response[pb.MarkRedeemedKeysResponse], error) {
	n, err := h.catalog.MarkRedeemedKeys(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.MarkRedeemedKeysResponse{Updated: int32(n)}), nil
}

// ListLinkStores lists the stores games can be linked to: those the media providers read links of
// (in chain order), then those only sources link games to.
func (h *GameHandler) ListLinkStores(context.Context, *connect.Request[pb.ListLinkStoresRequest]) (*connect.Response[pb.ListLinkStoresResponse], error) {
	out := &pb.ListLinkStoresResponse{}
	byKey := map[string]*pb.LinkStore{}
	add := func(s game.Store, searchable bool) {
		if ls, ok := byKey[s.Key]; ok {
			if ls.PageUrl == "" {
				ls.PageUrl = s.PageURL
			}

			return
		}

		byKey[s.Key] = &pb.LinkStore{
			Key:        s.Key,
			Name:       s.Name,
			PageUrl:    s.PageURL,
			Searchable: searchable,
		}
		out.Stores = append(out.Stores, byKey[s.Key])
	}

	for _, s := range h.media.LinkStores() {
		add(s.Store, s.Searchable)
	}

	for _, s := range h.sources.LinkStores() {
		add(s, false)
	}

	return connect.NewResponse(out), nil
}

// SearchLinks searches a store's catalog by title, to link a game to it.
func (h *GameHandler) SearchLinks(ctx context.Context, req *connect.Request[pb.SearchLinksRequest]) (*connect.Response[pb.SearchLinksResponse], error) {
	matches, err := h.media.SearchLinks(ctx, req.Msg.Store, req.Msg.Query)
	if errors.Is(err, media.ErrUnknownStore) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}

	out := &pb.SearchLinksResponse{}
	for _, m := range matches {
		out.Matches = append(out.Matches, &pb.LinkMatch{
			Id:       m.ID,
			Name:     m.Name,
			ImageUrl: m.ImageURL,
		})
	}

	return connect.NewResponse(out), nil
}
