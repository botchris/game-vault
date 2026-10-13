package rpc_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/inbound/rpc"
	"gamevault/internal/adapters/outbound/csvfile"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/transfer"
	pb "gamevault/internal/gen/gamevault/v1"
)

func TestImportCsv_codes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// importer returns a handler whose database is open, or already closed so every query fails.
	importer := func(t *testing.T, closed bool) *rpc.SystemHandler {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)

		if closed {
			require.NoError(t, db.Close())
		} else {
			t.Cleanup(func() { db.Close() })
		}

		return rpc.NewSystemHandler(nil, transfer.NewService(sqlite.NewGameRepository(db), catalog.NewService(sqlite.NewGameRepository(db), db),
			sqlite.NewSettingsRepository(db), csvfile.Codec{}))
	}

	t.Run("GIVEN a working catalog", func(t *testing.T) {
		h := importer(t, false)

		t.Run("WHEN the file has no title column", func(t *testing.T) {
			_, err := h.ImportCsv(ctx, connect.NewRequest(&pb.ImportCsvRequest{Content: []byte("name,kind\nOkami,physical\n")}))

			t.Run("THEN it is invalid input", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})

		t.Run("WHEN the file is not CSV at all", func(t *testing.T) {
			_, err := h.ImportCsv(ctx, connect.NewRequest(&pb.ImportCsvRequest{Content: []byte("title\n\"Okami")}))

			t.Run("THEN it is invalid input", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})
	})

	t.Run("GIVEN a catalog whose database fails", func(t *testing.T) {
		h := importer(t, true)

		t.Run("WHEN a valid file is imported", func(t *testing.T) {
			_, err := h.ImportCsv(ctx, connect.NewRequest(&pb.ImportCsvRequest{Content: []byte("title,kind\nOkami,physical\n")}))

			t.Run("THEN it is an internal error, not the file's fault", func(t *testing.T) {
				assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
			})
		})
	})
}
