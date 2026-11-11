package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxUserBody = 64 << 10

// Handler принимает HTTP-запросы пользователей и ролей и передаёт работу сервису.
type Handler struct{ service *Service }

// NewHandler создаёт HTTP-обработчик, использующий сервис пользователей.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// RegisterPublicRoutes регистрирует открытый маршрут просмотра ролей.
func (h *Handler) RegisterPublicRoutes(router chi.Router) {
	router.Get("/role", h.listRoles)
}

// RegisterAdminRoutes регистрирует операции с пользователями и ролями для защищённой группы администратора.
func (h *Handler) RegisterAdminRoutes(router chi.Router) {
	router.Post("/user", h.createUser)
	router.Get("/user", h.listUsers)
	router.Get("/user/role/{id}", h.listUsersByRole)
	router.Put("/user/{id}", h.updateUser)
	router.Delete("/user/{id}", h.deleteUser)
	router.Post("/role", h.createRole)
	router.Put("/role/{id}", h.updateRole)
	router.Delete("/role/{id}", h.deleteRole)
}

// createUser читает данные пользователя из JSON, вызывает сервис и возвращает созданную запись.
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	item, err := h.service.Create(r.Context(), input)
	if err != nil {
		writeUserError(w, err)
		return
	}
	w.Header().Set("Location", "/user/"+item.ID)
	writeJSON(w, http.StatusCreated, item)
}

// listUsers возвращает страницу пользователей с параметрами из URL.
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	page, err := h.service.List(r.Context(), r.URL.Query(), "")
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// listUsersByRole возвращает страницу пользователей с ролью из адреса запроса.
func (h *Handler) listUsersByRole(w http.ResponseWriter, r *http.Request) {
	page, err := h.service.List(r.Context(), r.URL.Query(), chi.URLParam(r, "id"))
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// updateUser читает новые поля профиля и передаёт их сервису для проверки и сохранения.
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	var input UpdateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	item, err := h.service.Update(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// deleteUser удаляет пользователя по UUID и возвращает пустой успешный ответ.
func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeUserError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listRoles возвращает список доступных ролей.
func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListRoles(r.Context())
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// createRole читает название роли из JSON и возвращает результат её создания.
func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	var input RoleInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	item, err := h.service.CreateRole(r.Context(), input)
	if err != nil {
		writeUserError(w, err)
		return
	}
	w.Header().Set("Location", "/role/"+item.ID)
	writeJSON(w, http.StatusCreated, item)
}

// updateRole передаёт UUID роли и новое название сервису для проверки и изменения.
func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	var input RoleInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	item, err := h.service.UpdateRole(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// deleteRole удаляет роль по UUID и сообщает об ошибках через HTTP-ответ.
func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRole(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeUserError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON проверяет тип и размер тела запроса и разбирает один JSON-объект без неизвестных полей.
// Форма передаёт только данные соответствующей операции. Неизвестное поле отклоняется,
// чтобы ошибка в имени параметра не приводила к молчаливому сохранению неполного профиля.
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUserBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("body must contain one JSON value")
	}
	return nil
}

// writeUserError выбирает безопасный HTTP-код для ошибки сервиса пользователей или ролей.
func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "user or role not found")
	case errors.Is(err, ErrHasOrders):
		writeError(w, http.StatusConflict, "user has order history and cannot be deleted")
	case errors.Is(err, ErrLastAdmin), errors.Is(err, ErrSystemRole), errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "operation conflicts with protected account or role data")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// writeError формирует JSON-ответ с кодом и текстом ошибки.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeJSON кодирует значение в JSON и отправляет его с указанным HTTP-кодом.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
