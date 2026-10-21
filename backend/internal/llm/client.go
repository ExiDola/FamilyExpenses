package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Intent struct {
	Action         string   `json:"action"`
	Amount         *float64 `json:"amount"`
	Category       string   `json:"category"`
	SpentAt        string   `json:"spent_at"`
	DateFrom       string   `json:"date_from"`
	DateTo         string   `json:"date_to"`
	FilterCategory string   `json:"filter_category"`
	Comment        string   `json:"comment"`
	SearchText     string   `json:"search_text"`
	TargetLatest   bool     `json:"target_latest"`
	OldAmount      *float64 `json:"old_amount"`
	NewAmount      *float64 `json:"new_amount"`
	NewCategory    string   `json:"new_category"`
	NewSpentAt     string   `json:"new_spent_at"`
	NewComment     string   `json:"new_comment"`
	Message        string   `json:"message"`
}

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTP: &http.Client{Timeout: 45 * time.Second}}
}

func (c *Client) Enabled() bool {
	return c.APIKey != "" && c.Model != ""
}

func (c *Client) Interpret(ctx context.Context, text, today string) (Intent, error) {
	if !c.Enabled() {
		return Intent{}, errors.New("LLM не настроена: заполните LLM_API_KEY и LLM_MODEL в .env")
	}
	system := `Ты — парсер команд для семейного учёта расходов. Верни только JSON-объект, без markdown. Не выполняй запросы и не придумывай факты. Все даты — YYYY-MM-DD. Сегодня: ` + today + `.
Доступные категории: еда, транспорт, жильё, развлечения, здоровье, покупки, другое.
Поля JSON: action (add|list|sum|update|delete|help), amount (число или null), category (категория для добавления/изменения или пустая строка), spent_at (дата новой траты или пустая строка), date_from (начало периода или пустая строка), date_to (конец периода или пустая строка), filter_category (категория фильтра или пустая строка), comment (описание новой траты или пустая строка), search_text (слова для поиска целевой траты или пустая строка), target_latest (true только если пользователь явно ссылается на последнюю трату), old_amount (старая сумма для поиска при исправлении или null), new_amount (новая сумма при изменении или null), new_category (новая категория при изменении или пустая строка), new_spent_at (новая дата при изменении или пустая строка), new_comment (новый комментарий при изменении или пустая строка), message (краткий ответ пользователю при необходимости уточнения).
Правила: «кофе 350» — add, amount=350, category=еда, comment=кофе, spent_at=сегодня. «такси 1200 вчера» — add, category=транспорт, spent_at=вчера, comment=такси. Относительные даты вычисляй относительно сегодняшней даты. «сколько потратил на еду в этом месяце» — sum и точный диапазон текущего календарного месяца. «покажи траты за неделю» — list и диапазон последних 7 календарных дней включая сегодня. Для list и sum заполняй filter_category, а не category. «последнее было не 350, а 450» — update, target_latest=true, old_amount=350, new_amount=450. «удали такси» — delete и search_text=такси. Для update указанный новый атрибут положи в new_amount/new_category/new_spent_at/new_comment; search_text описывает старую трату. Если намерение непонятно, используй help и объясни коротко в message. Не создавай расходов при вопросе или просьбе показать траты.`
	user := `Сообщение: ` + text
	payload := map[string]any{
		"model":           c.Model,
		"messages":        []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"temperature":     0.1,
		"response_format": map[string]string{"type": "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Intent{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Intent{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Intent{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Intent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Intent{}, fmt.Errorf("LLM вернула HTTP %d: %s", resp.StatusCode, truncate(string(data), 400))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Intent{}, err
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return Intent{}, errors.New("LLM вернула пустой ответ")
	}
	var intent Intent
	if err := json.Unmarshal([]byte(result.Choices[0].Message.Content), &intent); err != nil {
		return Intent{}, fmt.Errorf("не удалось разобрать ответ LLM: %w", err)
	}
	intent.Action = strings.ToLower(strings.TrimSpace(intent.Action))
	intent.Category = strings.TrimSpace(intent.Category)
	intent.FilterCategory = strings.TrimSpace(intent.FilterCategory)
	intent.NewCategory = strings.TrimSpace(intent.NewCategory)
	intent.SearchText = strings.TrimSpace(intent.SearchText)
	return intent, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
