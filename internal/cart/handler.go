package cart

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RaniXst/clothing-store/internal/auth"
	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) RegisterRoutes(router chi.Router) {
	router.With(auth.CSRF).Post("/cart", h.add)
	router.Get("/cart/{user_id}", h.list)
	router.With(auth.CSRF).Put("/cart/{id}", h.update)
	router.With(auth.CSRF).Delete("/cart/{id}", h.delete)
}
func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var input AddInput
	if err := decode(w, r, &input); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	item, err := h.service.Add(r.Context(), actor(r), input)
	if err != nil {
		writeCartError(w, err)
		return
	}
	w.Header().Set("Location", "/cart/"+item.ID)
	writeJSON(w, 201, item)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), actor(r), chi.URLParam(r, "user_id"))
	if err != nil {
		writeCartError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Count int64 `json:"count"`
	}
	if err := decode(w, r, &input); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	item, err := h.service.Update(r.Context(), actor(r), chi.URLParam(r, "id"), input.Count)
	if err != nil {
		writeCartError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), actor(r), chi.URLParam(r, "id")); err != nil {
		writeCartError(w, err)
		return
	}
	w.WriteHeader(204)
}
func actor(r *http.Request) Actor {
	p, _ := auth.PrincipalFromContext(r.Context())
	return Actor{UserID: p.User.ID, Role: p.User.Role.Name}
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("content type must be json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}
func writeCartError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, 400, err.Error())
	case errors.Is(err, ErrDenied):
		writeError(w, 403, "insufficient permissions")
	case errors.Is(err, ErrNotFound):
		writeError(w, 404, "cart item not found")
	case errors.Is(err, ErrConflict):
		writeError(w, 409, "cart quantity exceeds the supported range")
	default:
		writeError(w, 500, "internal server error")
	}
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
