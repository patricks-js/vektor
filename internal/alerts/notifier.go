package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type WebhookPayload struct {
	Source    string  `json:"source"`
	Status    string  `json:"status"`
	RuleName  string  `json:"rule_name"`
	Resource  string  `json:"resource_id"`
	Metric    string  `json:"metric_key"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Message   string  `json:"message"`
	Text      string  `json:"text"`
	Timestamp string  `json:"timestamp"`
}

type Notifier struct {
	client *http.Client
}

func NewNotifier() *Notifier {
	return &Notifier{
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (n *Notifier) SendWebhook(ctx context.Context, targetURL string, payload WebhookPayload) error {
	if err := ValidateWebhookURL(targetURL); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(targetURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "vektord-alerts/1")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) TestURL(ctx context.Context, targetURL string) error {
	msg := "[Vektor] Notificação de teste enviada com sucesso."
	return n.SendWebhook(ctx, targetURL, WebhookPayload{
		Source:    "vektor",
		Status:    "test",
		RuleName:  "Teste de webhook",
		Resource:  "host:local",
		Metric:    "test",
		Message:   msg,
		Text:      msg,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (n *Notifier) NotifyAll(ctx context.Context, channels []Channel, ev Event) string {
	stateLabel := "ALERTA"
	if ev.Status == "resolved" {
		stateLabel = "RESOLVIDO"
	}
	msg := fmt.Sprintf("[Vektor %s] %s (%s / %s): valor %.2f (limite %.2f)",
		stateLabel, ev.RuleName, ev.ResourceID, ev.MetricKey, ev.Value, ev.Threshold)
	payload := WebhookPayload{
		Source:    "vektor",
		Status:    ev.Status,
		RuleName:  ev.RuleName,
		Resource:  ev.ResourceID,
		Metric:    ev.MetricKey,
		Value:     ev.Value,
		Threshold: ev.Threshold,
		Message:   msg,
		Text:      msg,
		Timestamp: time.UnixMilli(ev.TriggeredAtMs).UTC().Format(time.RFC3339),
	}

	var errs []string
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if err := n.SendWebhook(ctx, ch.URL, payload); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ch.Name, err))
		}
	}
	return strings.Join(errs, "; ")
}
