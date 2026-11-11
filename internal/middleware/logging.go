package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// Logging возвращает HTTP-посредник, который записывает метод, путь и длительность запроса.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			next.ServeHTTP(w, r)

			// Для диагностики достаточно метода, пути и времени ответа. Тело, заголовки и параметры
			// запроса могут содержать пароли, токены и данные пользователей.
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"duration", time.Since(start).String(),
			)
		})
	}
}
