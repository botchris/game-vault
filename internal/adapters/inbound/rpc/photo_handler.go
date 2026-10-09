package rpc

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

// photoUploadHeader must be on every upload. A page on another site cannot add it without a CORS
// preflight this server never grants, so it cannot upload photos through the browser of someone on
// a trusted network (where no session cookie is needed, so SameSite cookies do not protect).
const photoUploadHeader = "X-Gamevault-Upload"

// photoUploadHandler serves POST /media/photos: a multipart form with the parts "photo" and
// "thumb" (JPEG). It answers {"id": "…", "takenAt": "…"}; takenAt is left out when unknown.
func photoUploadHandler(m *media.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(photoUploadHeader) == "" {
			http.Error(w, "uploads need the "+photoUploadHeader+" header", http.StatusForbidden)
			return
		}

		const maxForm = media.MaxPhotoBytes + media.MaxThumbBytes + 1<<20 // parts plus the form's own overhead

		r.Body = http.MaxBytesReader(w, r.Body, maxForm)
		if err := r.ParseMultipartForm(maxForm); err != nil {
			http.Error(w, "invalid photo: the upload is not a form or is too large", http.StatusBadRequest)
			return
		}

		defer func() { _ = r.MultipartForm.RemoveAll() }() // temporary files only; nothing to report

		up, err := m.UploadPhoto(formPart(r, "photo"), formPart(r, "thumb"))
		switch {
		case errors.Is(err, media.ErrInvalidPhoto):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case err != nil:
			http.Error(w, "the photo could not be stored: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp := struct {
			ID      string `json:"id"`
			TakenAt string `json:"takenAt,omitempty"`
		}{
			ID: string(up.ID),
		}
		if !up.TakenAt.IsZero() {
			resp.TakenAt = up.TakenAt.Format(time.RFC3339)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp) // the client went away; nothing to do
	})
}

// formPart returns the content of a form file, nil when it is missing or unreadable (the use case
// then says which part is missing).
func formPart(r *http.Request, name string) []byte {
	f, _, err := r.FormFile(name)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }() // only read

	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	return data
}

// photoHandler serves GET /media/photos/{id} and, with thumb, GET /media/photos/{id}/thumb. A
// photo never changes (its id is its content's hash), so it is cached for good.
func photoHandler(m *media.Service, thumb bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img, err := m.Photo(game.PhotoID(r.PathValue("id")), thumb)
		switch {
		case errors.Is(err, media.ErrNoPhoto):
			http.Error(w, "no such photo", http.StatusNotFound)
			return
		case err != nil:
			http.Error(w, "photo unavailable", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", img.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(img.Data)
	})
}
