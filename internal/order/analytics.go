package order

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/RaniXst/clothing-store/internal/auth"
	"github.com/go-chi/chi/v5"
)

type analyticsSummary struct {
	Total              int64            `json:"total"`
	Cancelled          int64            `json:"cancelled"`
	NonCancelledAmount int64            `json:"non_cancelled_amount"`
	ByStatus           map[string]int64 `json:"by_status"`
}

// RegisterAnalytics attaches the staff-only summary to the authenticated router.
func RegisterAnalytics(router chi.Router, db *sql.DB) {
	router.With(auth.RequireRoles("admin", "manager")).Get("/order/analytics", func(w http.ResponseWriter, r *http.Request) {
		var from, until time.Time
		now := time.Now()
		// Периоды считаются по календарю сервера, а границы переводятся в UTC для сравнения
		// с датами в базе. Последние семь дней включают текущий день.
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		switch r.URL.Query().Get("period") {
		case "", "all":
		case "today":
			from, until = today, today.AddDate(0, 0, 1)
		case "week":
			from, until = today.AddDate(0, 0, -6), today.AddDate(0, 0, 1)
		default:
			writeOrderError(w, ErrInvalid)
			return
		}
		summary, err := summarizeOrders(r.Context(), db, from, until)
		if err != nil {
			writeOrderError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, summary)
	})
}

// Считаем все заказы периода в SQLite, независимо от открытой страницы списка.
// Деньги берутся из цен, записанных в позициях заказа. Отменённые входят в счётчик
// статусов, но их стоимость не прибавляется к сумме остальных заказов.
func summarizeOrders(ctx context.Context, db *sql.DB, from, until time.Time) (analyticsSummary, error) {
	query := `SELECT o.status, COUNT(*), COALESCE(SUM(CASE WHEN o.status <> ? THEN
		(SELECT COALESCE(SUM(i.price * i.count), 0) FROM order_items i WHERE i.order_id = o.id)
		ELSE 0 END), 0) FROM orders o`
	args := []any{StatusCancelled}
	if !from.IsZero() {
		const storedTime = "2006-01-02T15:04:05.000000000Z07:00"
		query += " WHERE o.datetime >= ? AND o.datetime < ?"
		args = append(args, from.UTC().Format(storedTime), until.UTC().Format(storedTime))
	}
	rows, err := db.QueryContext(ctx, query+" GROUP BY o.status", args...)
	if err != nil {
		return analyticsSummary{}, err
	}
	defer rows.Close()
	summary := analyticsSummary{ByStatus: make(map[string]int64, len(Statuses))}
	for _, status := range Statuses {
		summary.ByStatus[status] = 0
	}
	for rows.Next() {
		var status string
		var count, amount int64
		if err := rows.Scan(&status, &count, &amount); err != nil {
			return analyticsSummary{}, err
		}
		summary.Total += count
		summary.ByStatus[status] = count
		var err error
		summary.NonCancelledAmount, err = addAmount(summary.NonCancelledAmount, amount, 1)
		if err != nil {
			return analyticsSummary{}, err
		}
	}
	summary.Cancelled = summary.ByStatus[StatusCancelled]
	return summary, rows.Err()
}
