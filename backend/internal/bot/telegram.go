package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"family-expenses/backend/internal/domain"
	"family-expenses/backend/internal/llm"
	"family-expenses/backend/internal/repository"
)

type Telegram struct {
	token    string
	repo     *repository.Postgres
	llm      *llm.Client
	location *time.Location
	http     *http.Client
}

type update struct {
	UpdateID int64    `json:"update_id"`
	Message  *message `json:"message"`
}

type message struct {
	Text string `json:"text"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		IsBot     bool   `json:"is_bot"`
	} `json:"from"`
}

type telegramResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
}

func New(token string, repo *repository.Postgres, client *llm.Client, location *time.Location) *Telegram {
	return &Telegram{token: token, repo: repo, llm: client, location: location, http: &http.Client{Timeout: 40 * time.Second}}
}

func (t *Telegram) Run(ctx context.Context) {
	if t.token == "" {
		log.Print("Telegram bot disabled: TELEGRAM_BOT_TOKEN is empty")
		return
	}
	log.Print("Telegram bot long polling started")
	var offset int64
	for ctx.Err() == nil {
		endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=30&offset=%d", t.token, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		resp, err := t.http.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("Telegram polling error: %v", err)
				time.Sleep(3 * time.Second)
			}
			continue
		}
		var payload telegramResponse[[]update]
		decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if decodeErr != nil || !payload.OK {
			log.Printf("Telegram response error: %v %s", decodeErr, payload.Description)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, item := range payload.Result {
			offset = item.UpdateID + 1
			if item.Message != nil {
				if err := t.handle(ctx, item.Message); err != nil {
					log.Printf("Telegram message handling error: %v", err)
				}
			}
		}
	}
}

func (t *Telegram) handle(ctx context.Context, msg *message) error {
	log.Printf("Telegram message received: user_id=%d chat_type=%s text_len=%d", msg.From.ID, msg.Chat.Type, len(msg.Text))
	if msg.From.ID == 0 || msg.From.IsBot || msg.Chat.Type != "private" || strings.TrimSpace(msg.Text) == "" {
		return nil
	}
	member := domain.Member{TelegramID: msg.From.ID, Username: msg.From.Username, FirstName: msg.From.FirstName, LastName: msg.From.LastName}
	if err := t.repo.UpsertMember(ctx, member); err != nil {
		return err
	}
	input := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(input, "/start") || strings.HasPrefix(input, "/help") {
		return t.send(ctx, msg.Chat.ID, "Пишите расходы обычным текстом: «кофе 350», «такси 1200 вчера». Можно спросить «сколько потратил на еду в этом месяце», исправить «последнее было не 350, а 450» или удалить «удали такси».")
	}
	today := time.Now().In(t.location).Format("2006-01-02")
	intent, err := t.llm.Interpret(ctx, input, today)
	if err != nil {
		return t.send(ctx, msg.Chat.ID, "Не удалось разобрать сообщение. Проверьте настройки LLM и попробуйте ещё раз.")
	}
	answer, err := t.execute(ctx, msg.From.ID, intent)
	if err != nil {
		return t.send(ctx, msg.Chat.ID, "Не удалось выполнить запрос к базе данных. Попробуйте ещё раз.")
	}
	return t.send(ctx, msg.Chat.ID, answer)
}

func (t *Telegram) execute(ctx context.Context, userID int64, in llm.Intent) (string, error) {
	today := time.Now().In(t.location).Format("2006-01-02")
	switch in.Action {
	case "add":
		if in.Amount == nil || *in.Amount <= 0 {
			return "Укажите сумму, например: «кофе 350».", nil
		}
		category := normalizeCategory(in.Category)
		spentAt := validDate(in.SpentAt, today)
		expense, err := t.repo.CreateExpense(ctx, userID, *in.Amount, category, spentAt, strings.TrimSpace(in.Comment))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Добавлено: %s — %s, %.2f (%s).", commentOrCategory(expense), expense.Category, expense.Amount, expense.SpentAt.Format("02.01.2006")), nil
	case "list", "sum":
		filter := domain.Filter{UserID: &userID, From: validOptionalDate(in.DateFrom), To: validOptionalDate(in.DateTo), Category: normalizeOptionalCategory(in.FilterCategory), Limit: 30}
		if in.Action == "sum" {
			total, count, err := t.repo.SumExpenses(ctx, filter)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Итого: %.2f. Количество трат: %d.", total, count), nil
		}
		expenses, err := t.repo.ListExpenses(ctx, filter)
		if err != nil {
			return "", err
		}
		if len(expenses) == 0 {
			return "За указанный период трат не найдено.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Найдено трат: %d\n", len(expenses))
		for _, e := range expenses {
			fmt.Fprintf(&b, "%s · %.2f · %s · %s\n", e.SpentAt.Format("02.01"), e.Amount, e.Category, commentOrCategory(e))
		}
		return strings.TrimSpace(b.String()), nil
	case "update", "delete":
		category := normalizeOptionalCategory(in.FilterCategory)
		if in.Action == "update" && category == "" {
			category = normalizeOptionalCategory(in.Category)
		}
		candidates, err := t.repo.FindForUser(ctx, userID, in.SearchText, category, in.OldAmount, in.TargetLatest)
		if err != nil {
			return "", err
		}
		if len(candidates) == 0 {
			return "Подходящая трата не найдена. Уточните описание или сумму.", nil
		}
		selected := candidates[0]
		if in.TargetLatest && in.OldAmount != nil && selected.Amount != *in.OldAmount {
			return fmt.Sprintf("Последняя трата сейчас %.2f, а не %.2f. Уточните, какую запись изменить.", selected.Amount, *in.OldAmount), nil
		}
		if in.Action == "delete" {
			if err := t.repo.DeleteExpenseForUser(ctx, selected.ID, userID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Удалена трата: %.2f · %s · %s.", selected.Amount, selected.Category, commentOrCategory(selected)), nil
		}
		changed := false
		if in.NewAmount != nil && *in.NewAmount > 0 {
			selected.Amount = *in.NewAmount
			changed = true
		}
		if in.NewCategory != "" {
			selected.Category = normalizeCategory(in.NewCategory)
			changed = true
		}
		if in.NewSpentAt != "" {
			selected.SpentAt, _ = time.ParseInLocation("2006-01-02", validDate(in.NewSpentAt, today), t.location)
			changed = true
		}
		if in.NewComment != "" {
			selected.Comment = strings.TrimSpace(in.NewComment)
			changed = true
		}
		if in.Amount != nil && in.NewAmount == nil && *in.Amount > 0 {
			selected.Amount = *in.Amount
			changed = true
		}
		if !changed {
			return "Уточните, что изменить: сумму, категорию, дату или комментарий.", nil
		}
		saved, err := t.repo.UpdateExpense(ctx, selected)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Изменено: %.2f · %s · %s (%s).", saved.Amount, saved.Category, commentOrCategory(saved), saved.SpentAt.Format("02.01.2006")), nil
	case "help":
		if in.Message != "" {
			return in.Message, nil
		}
		return "Напишите, что хотите сделать: добавить трату, посмотреть расходы, изменить или удалить запись.", nil
	default:
		return "Не понял запрос. Например: «кофе 350», «покажи траты за неделю», «удали такси».", nil
	}
}

func (t *Telegram) send(ctx context.Context, chatID int64, text string) error {
	payload, err := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram sendMessage returned %d", resp.StatusCode)
	}
	return nil
}

func normalizeCategory(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "еда", "транспорт", "жильё", "развлечения", "здоровье", "покупки", "другое":
		return value
	case "жилье":
		return "жильё"
	case "food":
		return "еда"
	case "transport":
		return "транспорт"
	default:
		return "другое"
	}
}

func normalizeOptionalCategory(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return normalizeCategory(value)
}

func validDate(value, fallback string) string {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed.Format("2006-01-02")
}

func validOptionalDate(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}

func commentOrCategory(e domain.Expense) string {
	if strings.TrimSpace(e.Comment) != "" {
		return e.Comment
	}
	return e.Category
}
