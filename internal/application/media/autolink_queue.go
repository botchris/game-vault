package media

import (
	"context"
	"sync"

	"gamevault/internal/domain/game"
)

// autoLinkQueue is the set of games waiting for the automatic-links worker, in arrival order and
// without duplicates.
type autoLinkQueue struct {
	mu     sync.Mutex
	queued map[game.ID]struct{}
	order  []game.ID

	// ready has a token while the queue may have games, so the worker sleeps until one arrives.
	ready chan struct{}
}

func newAutoLinkQueue() *autoLinkQueue {
	return &autoLinkQueue{
		queued: map[game.ID]struct{}{},
		ready:  make(chan struct{}, 1),
	}
}

// push adds a game unless it is already waiting.
func (q *autoLinkQueue) push(id game.ID) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, ok := q.queued[id]; ok {
		return
	}

	q.queued[id] = struct{}{}
	q.order = append(q.order, id)

	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// pop takes the oldest game, if any.
func (q *autoLinkQueue) pop() (game.ID, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.order) == 0 {
		return "", false
	}

	id := q.order[0]
	q.order = q.order[1:]
	delete(q.queued, id)

	return id, true
}

func (q *autoLinkQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.order)
}

// linkQueue is where games are queued for the auto-linker: the AutoLinker, or nothing.
type linkQueue interface {
	// Enqueue queues a game whose links are due, without blocking.
	Enqueue(g *game.Game)

	// EnqueueDue queues every game whose links are due.
	EnqueueDue(ctx context.Context)
}

// noAutoLinks queues nothing: no links are looked for.
type noAutoLinks struct{}

func (noAutoLinks) Enqueue(*game.Game) {}

func (noAutoLinks) EnqueueDue(context.Context) {}
