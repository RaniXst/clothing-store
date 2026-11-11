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

// Handler принимает HTTP-запросы заказов и передаёт проверку правил службе заказов.
type Handler struct{ service *Service }

// NewHandler создаёт обработчик заказов.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }
// RegisterRoutes добавляет маршруты заказов; для изменения данных включает CSRF-проверку.
func (h *Handler) RegisterRoutes(router chi.Router) {
	router.With(auth.CSRF).Post("/order", h.checkout)
	router.Get("/order", h.list)
	router.Get("/order/{id}", h.get)
	router.With(auth.CSRF).Put("/order/{id}", h.updateStatus)
	router.With(auth.CSRF).Post("/order_item", h.addItem)
	router.Get("/order_item/{order_id}", h.items)
}

// checkout оформляет корзину вошедшего клиента и возвращает созданный заказ.
func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Checkout(r.Context(), actor(r))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	w.Header().Set("Location", "/order/"+item.ID)
	writeJSON(w, 201, item)
}
// list проверяет фильтры и возвращает доступную этому пользователю страницу заказов.
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
// get возвращает заказ по UUID, если служба разрешает пользователю его видеть.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Get(r.Context(), actor(r), chi.URLParam(r, "id"))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
// updateStatus читает новое название статуса, проверяет его и передаёт службе.
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
// addItem читает товар и количество, затем добавляет позицию в существующий заказ.
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
// items возвращает позиции заказа после проверки прав доступа.
func (h *Handler) items(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Items(r.Context(), actor(r), chi.URLParam(r, "order_id"))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
// actor переносит в службу UUID и роль пользователя из проверенного контекста запроса.
func actor(r *http.Request) Actor {
	p, _ := auth.PrincipalFromContext(r.Context())
	return Actor{UserID: p.User.ID, Role: p.User.Role.Name}
}
// decode проверяет Content-Type и размер тела, затем читает один JSON-объект без неизвестных полей.
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

// writeOrderError переводит ошибки заказов в HTTP-коды, не раскрывая внутренние ошибки базы.
// Конфликт отделён от неверного запроса: например, статус может быть допустимым сам по себе,
// но переход к нему уже недоступен из текущего состояния заказа.
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
// writeError отправляет JSON-объект с текстом ошибки и её HTTP-кодом.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
// writeJSON кодирует значение ответа и отправляет его с указанным HTTP-кодом.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
