package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"family-expenses/backend/internal/domain"
	"family-expenses/backend/internal/repository"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	repo     *repository.Postgres
	location *time.Location
}

func New(repo *repository.Postgres, location *time.Location) *Handler {
	return &Handler{repo: repo, location: location}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/users", h.users)
	mux.HandleFunc("GET /api/expenses", h.listExpenses)
	mux.HandleFunc("POST /api/expenses", h.createExpense)
	mux.HandleFunc("GET /api/expenses/{id}", h.getExpense)
	mux.HandleFunc("PUT /api/expenses/{id}", h.updateExpense)
	mux.HandleFunc("DELETE /api/expenses/{id}", h.deleteExpense)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	users, err := h.repo.ListMembers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить членов семьи")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) listExpenses(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := domain.Filter{From: query.Get("from"), To: query.Get("to"), Category: query.Get("category"), Limit: 1000}
	if value := query.Get("user_id"); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "Некорректный user_id")
			return
		}
		filter.UserID = &id
	}
	if filter.Category != "" && !validCategory(filter.Category) {
		writeError(w, http.StatusBadRequest, "Некорректная категория")
		return
	}
	if !validOptionalDate(filter.From) || !validOptionalDate(filter.To) {
		writeError(w, http.StatusBadRequest, "Даты должны иметь формат YYYY-MM-DD")
		return
	}
	if filter.From != "" && filter.To != "" && filter.From > filter.To {
		writeError(w, http.StatusBadRequest, "Начало периода позже окончания")
		return
	}
	expenses, err := h.repo.ListExpenses(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить траты")
		return
	}
	writeJSON(w, http.StatusOK, expenses)
}

func (h *Handler) getExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	expense, err := h.repo.GetExpense(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Трата не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось получить трату")
		return
	}
	writeJSON(w, http.StatusOK, expense)
}

func (h *Handler) createExpense(w http.ResponseWriter, r *http.Request) {
	var input domain.ExpenseInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	if err := h.validateInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	exists, err := h.repo.MemberExists(r.Context(), input.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось проверить члена семьи")
		return
	}
	if !exists {
		writeError(w, http.StatusBadRequest, "Сначала член семьи должен написать боту в Telegram")
		return
	}
	spentAt := input.SpentAt
	if spentAt == "" {
		spentAt = time.Now().In(h.location).Format("2006-01-02")
	}
	expense, err := h.repo.CreateExpense(r.Context(), input.UserID, input.Amount, input.Category, spentAt, strings.TrimSpace(input.Comment))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось создать трату")
		return
	}
	writeJSON(w, http.StatusCreated, expense)
}

func (h *Handler) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input domain.ExpenseInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	if err := h.validateInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := h.repo.GetExpense(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Трата не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось получить трату")
		return
	}
	exists, err := h.repo.MemberExists(r.Context(), input.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось проверить члена семьи")
		return
	}
	if !exists {
		writeError(w, http.StatusBadRequest, "Член семьи не найден")
		return
	}
	spentAt := input.SpentAt
	if spentAt == "" {
		spentAt = current.SpentAt.Format("2006-01-02")
	}
	current.UserID = input.UserID
	current.Amount = input.Amount
	current.Category = input.Category
	current.Comment = strings.TrimSpace(input.Comment)
	current.SpentAt, _ = time.ParseInLocation("2006-01-02", spentAt, h.location)
	saved, err := h.repo.UpdateExpense(r.Context(), current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось изменить трату")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (h *Handler) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := h.repo.GetExpense(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Трата не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось получить трату")
		return
	}
	if err := h.repo.DeleteExpense(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить трату")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) validateInput(input *domain.ExpenseInput) error {
	if input.UserID <= 0 {
		return errors.New("Нужно выбрать члена семьи")
	}
	if input.Amount <= 0 {
		return errors.New("Сумма должна быть больше нуля")
	}
	if !validCategory(input.Category) {
		return errors.New("Некорректная категория")
	}
	if input.SpentAt != "" && !validOptionalDate(input.SpentAt) {
		return errors.New("Дата должна иметь формат YYYY-MM-DD")
	}
	if len(input.Comment) > 500 {
		return errors.New("Комментарий не должен превышать 500 символов")
	}
	return nil
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Некорректный ID")
		return 0, false
	}
	return id, true
}

func validCategory(value string) bool {
	for _, category := range domain.Categories {
		if value == category {
			return true
		}
	}
	return false
}

func validOptionalDate(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("ожидался один JSON-объект")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
