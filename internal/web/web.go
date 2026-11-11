// Package web serves the browser interface alongside the existing API.
package web

import (
	"database/sql"
	_ "embed"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

//go:embed index.html
var index string

//go:embed style.css
var css string

//go:embed store.js
var javascript string

// Register adds the storefront and a lookup for the latest photo of a product.
func Register(router chi.Router, db *sql.DB) {
	router.Get("/", asset("text/html; charset=utf-8", index))
	router.Get("/web/style.css", asset("text/css; charset=utf-8", css))
	router.Get("/web/store.js", asset("text/javascript; charset=utf-8", javascript))
	router.Get("/web/product-photo/{id}", func(w http.ResponseWriter, r *http.Request) {
		productID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		// Карточка запрашивает последнее фото по ID товара. Кэширование переадресации
		// могло бы оставить прежнюю фотографию после новой загрузки.
		w.Header().Set("Cache-Control", "no-store")
		var photoID string
		err = db.QueryRowContext(r.Context(),
			"SELECT id FROM photos WHERE product_id = ? ORDER BY rowid DESC LIMIT 1", productID.String(),
		).Scan(&photoID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			w.WriteHeader(http.StatusNoContent)
		case err != nil:
			http.Error(w, "photo lookup failed", http.StatusInternalServerError)
		default:
			http.Redirect(w, r, "/photo/"+photoID, http.StatusFound)
		}
	})
}

func asset(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		// Страница, стили и скрипт поставляются с приложением. Разрешение ресурсов со своего
		// адреса ограничивает исполнение сторонних скриптов и сохраняет локальный запуск.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'")
		_, _ = w.Write([]byte(body))
	}
}
