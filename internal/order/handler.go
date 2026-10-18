package order

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
	router.With(auth.CSRF).Post("/order", h.checkout)
	router.Get("/order", h.list)
	router.Get("/order/{id}", h.get)
	router.With(auth.CSRF).Put("/order/{id}", h.updateStatus)
	router.With(auth.CSRF).Post("/order_item", h.addItem)
	router.Get("/order_item/{order_id}", h.items)
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Checkout(r.Context(), actor(r))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	w.Header().Set("Location", "/order/"+item.ID)
	writeJSON(w, 201, item)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	options, err := ParseListOptions(r.URL.Query())
	if err != nil {
		writeOrderError(w, err)
		return
	}
	page, err := h.service.List(r.Context(), actor(r), options)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, 200, page)
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Get(r.Context(), actor(r), chi.URLParam(r, "id"))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request) {
	var input StatusInput
	if err := decode(w, r, &input); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if !IsStatus(input.Status) {
		writeOrderError(w, ErrInvalid)
		return
	}
	item, err := h.service.UpdateStatus(r.Context(), actor(r), chi.URLParam(r, "id"), input.Status)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (h *Handler) addItem(w http.ResponseWriter, r *http.Request) {
	var input AddItemInput
	if err := decode(w, r, &input); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	item, err := h.service.AddItem(r.Context(), actor(r), input)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	w.Header().Set("Location", "/order_item/"+item.OrderID)
	writeJSON(w, 201, item)
}
func (h *Handler) items(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Items(r.Context(), actor(r), chi.URLParam(r, "order_id"))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
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

func writeOrderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, 400, "invalid order request")
	case errors.Is(err, ErrDenied):
		writeError(w, 403, "insufficient permissions")
	case errors.Is(err, ErrNotFound):
		writeError(w, 404, "order not found")
	case errors.Is(err, ErrConflict):
		writeError(w, 409, "order state conflict")
	case errors.Is(err, ErrEmptyCart):
		writeError(w, 409, "cart is empty")
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
