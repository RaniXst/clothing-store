package photo

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

const multipartOverhead = 256 << 10

// Application перечисляет операции с фотографиями, необходимые HTTP-обработчику.
type Application interface {
	Create(context.Context, string, []byte) (Photo, error)
	Get(context.Context, string) (Photo, []byte, string, error)
	Delete(context.Context, string) error
}

// Handler принимает HTTP-запросы фотографий и вызывает службу проверки и хранения.
type Handler struct{ service Application }

// NewHandler создаёт HTTP-обработчик фотографий.
func NewHandler(service Application) *Handler { return &Handler{service: service} }

// RegisterRoutes добавляет публичное чтение и защищённые операции изменения фото.
func (h *Handler) RegisterRoutes(router chi.Router) {
	h.RegisterReadRoutes(router)
	h.RegisterWriteRoutes(router)
}

// RegisterReadRoutes добавляет публичный маршрут получения фотографии.
func (h *Handler) RegisterReadRoutes(router chi.Router) {
	router.Get("/photo/{id}", h.get)
}

// RegisterWriteRoutes добавляет загрузку и удаление фотографий для защищённой группы.
func (h *Handler) RegisterWriteRoutes(router chi.Router) {
	h.RegisterUploadRoute(router)
	h.RegisterDeleteRoute(router)
}

// RegisterUploadRoute adds the upload endpoint to the catalog editor group.
func (h *Handler) RegisterUploadRoute(router chi.Router) {
	router.Post("/photo", h.create)
}

// RegisterDeleteRoute adds the standalone photo deletion endpoint.
func (h *Handler) RegisterDeleteRoute(router chi.Router) {
	router.Delete("/photo/{id}", h.delete)
}

// create требует одно поле product_id и один файл, проверяет размер и передаёт изображение службе.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		writePhotoError(w, http.StatusBadRequest, "Content-Type must be multipart/form-data")
		return
	}
	// Ограничиваем весь multipart-запрос до разбора. Небольшой запас учитывает заголовки полей
	// и границы частей, которые не относятся к размеру самого изображения.
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize+multipartOverhead)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writePhotoError(w, http.StatusRequestEntityTooLarge, "multipart request is too large")
		} else {
			writePhotoError(w, http.StatusBadRequest, "invalid multipart form")
		}
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	for key := range r.MultipartForm.Value {
		if key != "product_id" {
			writePhotoError(w, http.StatusBadRequest, "unexpected multipart field")
			return
		}
	}
	for key := range r.MultipartForm.File {
		if key != "file" {
			writePhotoError(w, http.StatusBadRequest, "unexpected multipart file field")
			return
		}
	}
	productIDs := r.MultipartForm.Value["product_id"]
	files := r.MultipartForm.File["file"]
	if len(productIDs) != 1 || len(files) != 1 {
		writePhotoError(w, http.StatusBadRequest, "exactly one product_id and one file are required")
		return
	}
	upload, err := files[0].Open()
	if err != nil {
		writePhotoError(w, http.StatusBadRequest, "cannot read uploaded file")
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(upload, MaxUploadSize+1))
	closeErr := upload.Close()
	if readErr != nil || closeErr != nil {
		writePhotoError(w, http.StatusBadRequest, "cannot read uploaded file")
		return
	}
	if len(data) > MaxUploadSize {
		writePhotoError(w, http.StatusRequestEntityTooLarge, "image exceeds 5 MiB")
		return
	}
	item, err := h.service.Create(r.Context(), productIDs[0], data)
	if err != nil {
		writePhotoServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/photo/"+item.ID)
	writePhotoJSON(w, http.StatusCreated, item)
}

// get возвращает байты изображения и безопасные заголовки для его показа в браузере.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	item, data, contentType, err := h.service.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writePhotoServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", item.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// delete удаляет фотографию по UUID и возвращает пустой успешный ответ.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writePhotoServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writePhotoServiceError выбирает HTTP-ответ для известных ошибок службы фотографий.
func writePhotoServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writePhotoError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writePhotoError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, ErrProductAbsent):
		writePhotoError(w, http.StatusNotFound, "product not found")
	default:
		writePhotoError(w, http.StatusInternalServerError, "internal server error")
	}
}

// writePhotoError отправляет текст ошибки в JSON с указанным HTTP-кодом.
func writePhotoError(w http.ResponseWriter, status int, message string) {
	writePhotoJSON(w, status, map[string]string{"error": message})
}

// writePhotoJSON кодирует значение в JSON и записывает HTTP-код ответа.
func writePhotoJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
