package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// Telegram sends new matches to a chat. Disabled unless both
// TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are set.
type Telegram struct {
	token  string
	chatID string
}

func NewTelegram() *Telegram {
	return &Telegram{
		token:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		chatID: os.Getenv("TELEGRAM_CHAT_ID"),
	}
}

func (t *Telegram) Enabled() bool { return t.token != "" && t.chatID != "" }

func (t *Telegram) NotifyNew(ctx context.Context, jobs []model.Job) error {
	if !t.Enabled() || len(jobs) == 0 {
		return nil
	}
	const maxListed = 10
	text := fmt.Sprintf("🔍 %d new job match(es)\n\n", len(jobs))
	for i, j := range jobs {
		if i == maxListed {
			text += fmt.Sprintf("… and %d more\n", len(jobs)-maxListed)
			break
		}
		text += fmt.Sprintf("• [%d] %s — %s (%s, %s)\n%s\n\n", j.Score, j.Title, j.Company, j.Fit, j.Source, j.URL)
	}

	payload, err := json.Marshal(map[string]any{
		"chat_id":                  t.chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	url := "https://api.telegram.org/bot" + t.token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: %s", resp.Status)
	}
	return nil
}
