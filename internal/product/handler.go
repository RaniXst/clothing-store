package product

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxJSONBody = 1 << 20

// Application перечисляет операции каталога, которые нужны HTTP-обработчику.
type Application interface {
	Create(context.Context, ProductInput) (Product, error)
	Get(context.Context, string) (Product, error)
	Update(context.Context, string, ProductInput) (Product, error)
	Delete(context.Context, string) error
	List(context.Context, ListOptions) (ProductPage, error)
}

// Handler принимает HTTP-запросы каталога и вызывает соответствующий метод службы.
type Handler struct{ service Application }

// NewHandler создаёт HTTP-обработчик каталога.
func NewHandler(service Application) *Handler { return &Handler{service: service} }

// RegisterRoutes регистрирует все маршруты товаров.
func (h *Handler) RegisterRoutes(router chi.Router) {
	h.RegisterReadRoutes(router)
	h.RegisterWriteRoutes(router)
}

// RegisterReadRoutes добавляет публичные маршруты просмотра каталога.
func (h *Handler) RegisterReadRoutes(router chi.Router) {
	router.Get("/product", h.list)
	router.Get("/product/{id}", h.get)
}

// RegisterWriteRoutes добавляет маршруты изменения каталога для защищённой группы.
func (h *Handler) RegisterWriteRoutes(router chi.Router) {
	router.Post("/product", h.create)
	router.Put("/product/{id}", h.update)
	router.Delete("/product/{id}", h.delete)
}

// create читает JSON товара и возвращает созданный товар с кодом 201.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var input ProductInput
	if err := decodeProductInput(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.service.Create(r.Context(), input)
	if err != nil {
		writeProductError(w, err)
		return
	}
	w.Header().Set("Location", "/product/"+item.ID)
	writeJSON(w, http.StatusCreated, item)
}

// list проверяет параметры фильтрации и возвращает страницу каталога.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	options, err := ParseListOptions(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := h.service.List(r.Context(), options)
	if err != nil {
		writeProductError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// get возвращает один товар по UUID из адреса.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeProductError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// update читает полные данные товара и заменяет ими текущие поля.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var input ProductInput
	if err := decodeProductInput(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.service.Update(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		writeProductError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// delete удаляет товар, если он не нужен истории заказов.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeProductError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeProductInput проверяет тип и размер тела запроса и читает один JSON-объект товара.
// Ограничение тела и отказ от неизвестных полей ловят ошибку клиента до изменения товара.
// При PUT принимается полная карточка, чтобы у обновления не было неявных полей.
func decodeProductInput(w http.ResponseWriter, r *http.Request, input *ProductInput) error {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		return fmt.Errorf("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return nil
}

// writeProductError переводит ошибки каталога в подходящие HTTP-коды и сообщения.
func writeProductError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "product not found")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "product is referenced by order history")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// writeError формирует JSON-ответ с сообщением об ошибке.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeJSON кодирует ответ каталога и отправляет его с указанным HTTP-кодом.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
