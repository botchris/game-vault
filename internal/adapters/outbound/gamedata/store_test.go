package gamedata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gamevault/internal/application/media"
)

var jpg = media.Image{
	Data:        []byte("\xff\xd8\xff fake jpeg"),
	ContentType: "image/jpeg",
}

func TestLayoutRenameAndPrune(t *testing.T) {
	root := t.TempDir()

	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	g := media.GameRef{
		ID:    "019b11c2-aaaa",
		Title: `Halo 3: ODST / "Collector's"`,
	}
	if err := s.PutCover(g, "PS3", jpg); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, `Halo 3 ODST Collector's [019b11c2-aaaa]`)
	if covers := filesLike(t, dir, "cover-ps3-*.jpg"); len(covers) != 1 {
		t.Fatalf("cover not in the game's folder: %v", covers)
	}

	// Sheet images and their sources; removing one from the sheet deletes its file.
	if err := s.SetAssetSources(g, map[string]string{"screenshot-1": "https://x.test/1.jpg", "thumb-1": "https://x.test/1t.jpg"}); err != nil {
		t.Fatal(err)
	}

	s.PutAsset(g, "screenshot-1", jpg)
	s.PutAsset(g, "thumb-1", jpg)

	if u, ok := s.AssetSource(g.ID, "thumb-1"); !ok || u != "https://x.test/1t.jpg" {
		t.Fatalf("source not recorded: %q", u)
	}

	s.SetAssetSources(g, map[string]string{"screenshot-1": "https://x.test/1.jpg"})

	if _, ok, _ := s.GetAsset(g.ID, "thumb-1"); ok {
		t.Fatal("images no longer in the sheet must be pruned")
	}

	if _, ok, _ := s.GetCover(g.ID, "PS3"); !ok {
		t.Fatal("pruning must never touch the cover")
	}

	// Renaming the game renames its folder; reopening finds it by id.
	g.Title = "Halo 3: ODST"
	s.PutAsset(g, "screenshot-1", jpg)

	if _, err := os.Stat(filepath.Join(root, "Halo 3 ODST [019b11c2-aaaa]", "screenshot-1.jpg")); err != nil {
		t.Fatalf("folder not renamed: %v", err)
	}

	s2, _ := Open(root)
	if _, ok, _ := s2.GetAsset(g.ID, "screenshot-1"); !ok {
		t.Fatal("reopened store must find the folder by id")
	}

	if u, ok := s2.AssetSource(g.ID, "screenshot-1"); !ok || u == "" {
		t.Fatal("sources must survive a restart (assets.json)")
	}

	// Missing marker, path traversal, delete.
	s2.MarkCoverMissing(g, "PS3", time.Now())

	if _, ok := s2.CoverMissingSince(g.ID, "PS3"); !ok {
		t.Fatal("missing marker")
	}

	if _, _, err := s2.GetAsset(g.ID, "../../etc/passwd"); err == nil {
		t.Fatal("bad names must be rejected")
	}

	if err := s2.DeleteAll(g.ID); err != nil {
		t.Fatal(err)
	}

	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("game folder not deleted: %v", entries)
	}
}
