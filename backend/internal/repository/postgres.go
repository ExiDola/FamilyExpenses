package repository

import (
	"context"
	"fmt"
	"strings"

	"family-expenses/backend/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) UpsertMember(ctx context.Context, member domain.Member) error {
	display := strings.TrimSpace(strings.TrimSpace(member.FirstName) + " " + strings.TrimSpace(member.LastName))
	if display == "" && member.Username != "" {
		display = "@" + member.Username
	}
	if display == "" {
		display = fmt.Sprintf("Пользователь %d", member.TelegramID)
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO family_members (telegram_id, username, first_name, last_name, display_name)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (telegram_id) DO UPDATE SET username = EXCLUDED.username, first_name = EXCLUDED.first_name,
        last_name = EXCLUDED.last_name, display_name = EXCLUDED.display_name, updated_at = NOW()`,
		member.TelegramID, member.Username, member.FirstName, member.LastName, display)
	return err
}

func (p *Postgres) ListMembers(ctx context.Context) ([]domain.Member, error) {
	rows, err := p.pool.Query(ctx, `SELECT telegram_id, username, first_name, last_name, display_name FROM family_members ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]domain.Member, 0)
	for rows.Next() {
		var m domain.Member
		if err := rows.Scan(&m.TelegramID, &m.Username, &m.FirstName, &m.LastName, &m.DisplayName); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (p *Postgres) MemberExists(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM family_members WHERE telegram_id = $1)`, userID).Scan(&exists)
	return exists, err
}

func (p *Postgres) CreateExpense(ctx context.Context, userID int64, amount float64, category, spentAt, comment string) (domain.Expense, error) {
	return scanExpense(p.pool.QueryRow(ctx, `INSERT INTO expenses (user_id, amount, category, spent_at, comment)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, user_id, amount, category, spent_at, comment, created_at, updated_at`, userID, amount, category, spentAt, comment))
}

func (p *Postgres) GetExpense(ctx context.Context, id int64) (domain.Expense, error) {
	return scanExpense(p.pool.QueryRow(ctx, `SELECT id, user_id, amount, category, spent_at, comment, created_at, updated_at FROM expenses WHERE id = $1`, id))
}

func (p *Postgres) UpdateExpense(ctx context.Context, expense domain.Expense) (domain.Expense, error) {
	return scanExpense(p.pool.QueryRow(ctx, `UPDATE expenses SET user_id = $2, amount = $3, category = $4, spent_at = $5, comment = $6, updated_at = NOW()
        WHERE id = $1 RETURNING id, user_id, amount, category, spent_at, comment, created_at, updated_at`,
		expense.ID, expense.UserID, expense.Amount, expense.Category, expense.SpentAt.Format("2006-01-02"), expense.Comment))
}

func (p *Postgres) DeleteExpense(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	return err
}

func (p *Postgres) DeleteExpenseForUser(ctx context.Context, id, userID int64) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (p *Postgres) ListExpenses(ctx context.Context, filter domain.Filter) ([]domain.Expense, error) {
	query := `SELECT id, user_id, amount, category, spent_at, comment, created_at, updated_at FROM expenses WHERE TRUE`
	args := make([]any, 0, 5)
	if filter.UserID != nil {
		args = append(args, *filter.UserID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}
	if filter.From != "" {
		args = append(args, filter.From)
		query += fmt.Sprintf(" AND spent_at >= $%d", len(args))
	}
	if filter.To != "" {
		args = append(args, filter.To)
		query += fmt.Sprintf(" AND spent_at <= $%d", len(args))
	}
	if filter.Category != "" {
		args = append(args, filter.Category)
		query += fmt.Sprintf(" AND category = $%d", len(args))
	}
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY spent_at DESC, created_at DESC LIMIT $%d", len(args))
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	expenses := make([]domain.Expense, 0)
	for rows.Next() {
		item, err := scanExpenseFromRows(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, item)
	}
	return expenses, rows.Err()
}

func (p *Postgres) SumExpenses(ctx context.Context, filter domain.Filter) (float64, int64, error) {
	query := `SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM expenses WHERE TRUE`
	args := make([]any, 0, 4)
	if filter.UserID != nil {
		args = append(args, *filter.UserID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}
	if filter.From != "" {
		args = append(args, filter.From)
		query += fmt.Sprintf(" AND spent_at >= $%d", len(args))
	}
	if filter.To != "" {
		args = append(args, filter.To)
		query += fmt.Sprintf(" AND spent_at <= $%d", len(args))
	}
	if filter.Category != "" {
		args = append(args, filter.Category)
		query += fmt.Sprintf(" AND category = $%d", len(args))
	}
	var total float64
	var count int64
	err := p.pool.QueryRow(ctx, query, args...).Scan(&total, &count)
	return total, count, err
}

func (p *Postgres) FindForUser(ctx context.Context, userID int64, search, category string, oldAmount *float64, latest bool) ([]domain.Expense, error) {
	query := `SELECT id, user_id, amount, category, spent_at, comment, created_at, updated_at FROM expenses WHERE user_id = $1`
	args := []any{userID}
	if search != "" && !latest {
		args = append(args, "%"+strings.ToLower(search)+"%")
		query += fmt.Sprintf(" AND (LOWER(comment) LIKE $%d OR LOWER(category) LIKE $%d)", len(args), len(args))
	}
	if category != "" {
		args = append(args, category)
		query += fmt.Sprintf(" AND category = $%d", len(args))
	}
	if oldAmount != nil && !latest {
		args = append(args, *oldAmount)
		query += fmt.Sprintf(" AND amount = $%d", len(args))
	}
	query += " ORDER BY spent_at DESC, created_at DESC LIMIT 20"
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Expense, 0)
	for rows.Next() {
		item, err := scanExpenseFromRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanExpense(row interface{ Scan(...any) error }) (domain.Expense, error) {
	var e domain.Expense
	err := row.Scan(&e.ID, &e.UserID, &e.Amount, &e.Category, &e.SpentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanExpenseFromRows(row rowScanner) (domain.Expense, error) {
	return scanExpense(row)
}
