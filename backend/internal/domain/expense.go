package domain

import "time"

var Categories = []string{"еда", "транспорт", "жильё", "развлечения", "здоровье", "покупки", "другое"}

type Expense struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    float64   `json:"amount"`
	Category  string    `json:"category"`
	SpentAt   time.Time `json:"spent_at"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ExpenseInput struct {
	UserID   int64   `json:"user_id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	SpentAt  string  `json:"spent_at"`
	Comment  string  `json:"comment"`
}

type Member struct {
	TelegramID  int64  `json:"telegram_id"`
	Username    string `json:"username"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
}

type Filter struct {
	UserID   *int64
	From     string
	To       string
	Category string
	Limit    int
}
