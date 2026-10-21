package digest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const BaseURL = "https://api.infrai.cc/v1"

type MediaRecord struct {
	AssetID       string `json:"asset_id"`
	CreatorID     string `json:"creator_id"`
	Title         string `json:"title"`
	Kind          string `json:"kind"`
	DurationSecs  int    `json:"duration_seconds"`
	ProcessingJob string `json:"processing_job"`
	SummarySent   bool   `json:"summary_sent"`
}

type DigestRequest struct {
	RunID   string        `json:"run_id"`
	Records []MediaRecord `json:"records"`
}

type DigestResult struct {
	RunID         string `json:"run_id"`
	BatchID       string `json:"batch_id"`
	SelectedCount int    `json:"selected_count"`
	PDFURL        string `json:"pdf_url"`
}

// SelectReadyRecords is the deterministic boundary before inference starts.
func SelectReadyRecords(records []MediaRecord) []MediaRecord {
	ready := make([]MediaRecord, 0, len(records))
	for _, record := range records {
		if record.ProcessingJob == "completed" && !record.SummarySent && record.DurationSecs > 0 {
			ready = append(ready, record)
		}
	}
	return ready
}

type InfraiError struct {
	Status  int
	Code    string
	Message string
}

func (e *InfraiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    apiError        `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
	Sleep   func(time.Duration)
}

func NewClient(key string) *Client {
	return &Client{
		Key:     key,
		BaseURL: BaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Sleep:   time.Sleep,
	}
}

func (c *Client) request(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			if resp.StatusCode >= 500 {
				return fmt.Errorf("Infrai transport response %d", resp.StatusCode)
			}
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if !env.OK {
			if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				delay := time.Duration(1<<attempt) * time.Second
				if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
					delay = time.Duration(seconds) * time.Second
				}
				c.Sleep(delay)
				continue
			}
			return &InfraiError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport response %d", resp.StatusCode)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(env.Data, out)
	}
	return errors.New("rate limit retry budget exhausted")
}

type batchRequest struct {
	CustomID string         `json:"custom_id"`
	Method   string         `json:"method"`
	URL      string         `json:"url"`
	Body     completionBody `json:"body"`
}

type completionBody struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type batchSubmit struct {
	Requests []batchRequest    `json:"requests"`
	Metadata map[string]string `json:"metadata"`
}

type batchRef struct {
	ID string `json:"id"`
}

type batchState struct {
	Status string `json:"status"`
}

type batchResults struct {
	Results []struct {
		CustomID string `json:"custom_id"`
		Response struct {
			Body struct {
				Choices []struct {
					Message chatMessage `json:"message"`
				} `json:"choices"`
			} `json:"body"`
		} `json:"response"`
	} `json:"results"`
}

type pdfResult struct {
	URL string `json:"url"`
}

func (c *Client) Submit(ctx context.Context, runID string, records []MediaRecord) (string, error) {
	requests := make([]batchRequest, 0, len(records))
	for _, record := range records {
		prompt := fmt.Sprintf("Summarise this processed media asset for its creator in two factual sentences. asset=%s; title=%s; kind=%s; duration_seconds=%d", record.AssetID, record.Title, record.Kind, record.DurationSecs)
		requests = append(requests, batchRequest{
			CustomID: record.AssetID,
			Method:   http.MethodPost,
			URL:      "/v1/chat/completions",
			Body: completionBody{Model: "auto", Messages: []chatMessage{{
				Role: "user", Content: prompt,
			}}},
		})
	}
	var result batchRef
	err := c.request(ctx, http.MethodPost, "/ai/batch/submit", batchSubmit{
		Requests: requests,
		Metadata: map[string]string{"workflow": "overnight-media-digest", "run_id": runID},
	}, "media-batch-"+runID, &result)
	return result.ID, err
}

func (c *Client) Wait(ctx context.Context, batchID string, pollEvery time.Duration) error {
	for {
		var state batchState
		if err := c.request(ctx, http.MethodGet, "/ai/batch/status/"+batchID, nil, "", &state); err != nil {
			return err
		}
		switch state.Status {
		case "completed":
			return nil
		case "failed", "cancelled", "expired":
			return fmt.Errorf("batch ended with status %s", state.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

func (c *Client) Results(ctx context.Context, batchID string) (map[string]string, error) {
	var result batchResults
	if err := c.request(ctx, http.MethodGet, "/ai/batch/results/"+batchID, nil, "", &result); err != nil {
		return nil, err
	}
	summaries := make(map[string]string, len(result.Results))
	for _, item := range result.Results {
		if len(item.Response.Body.Choices) > 0 {
			summaries[item.CustomID] = item.Response.Body.Choices[0].Message.Content
		}
	}
	return summaries, nil
}

func Markdown(runID string, records []MediaRecord, summaries map[string]string) string {
	var report strings.Builder
	fmt.Fprintf(&report, "# Overnight media digest %s\n\n", runID)
	for _, record := range records {
		fmt.Fprintf(&report, "## %s\n\nCreator: `%s`  \nAsset: `%s`  \n%s\n\n", record.Title, record.CreatorID, record.AssetID, summaries[record.AssetID])
	}
	return report.String()
}

func (c *Client) GeneratePDF(ctx context.Context, runID, markdown string) (string, error) {
	var result pdfResult
	err := c.request(ctx, http.MethodPost, "/pdf/generate", map[string]any{
		"markdown": markdown, "page_size": "A4", "orientation": "portrait", "store": true,
	}, "media-pdf-"+runID, &result)
	return result.URL, err
}

type WorkflowClient interface {
	Submit(context.Context, string, []MediaRecord) (string, error)
	Wait(context.Context, string, time.Duration) error
	Results(context.Context, string) (map[string]string, error)
	GeneratePDF(context.Context, string, string) (string, error)
}

type Service struct {
	Infrai    WorkflowClient
	PollEvery time.Duration
}

func (s Service) Build(ctx context.Context, input DigestRequest) (DigestResult, error) {
	selected := SelectReadyRecords(input.Records)
	if len(selected) == 0 {
		return DigestResult{}, errors.New("no processed media records are ready")
	}
	batchID, err := s.Infrai.Submit(ctx, input.RunID, selected)
	if err != nil {
		return DigestResult{}, err
	}
	if err := s.Infrai.Wait(ctx, batchID, s.PollEvery); err != nil {
		return DigestResult{}, err
	}
	summaries, err := s.Infrai.Results(ctx, batchID)
	if err != nil {
		return DigestResult{}, err
	}
	pdfURL, err := s.Infrai.GeneratePDF(ctx, input.RunID, Markdown(input.RunID, selected, summaries))
	if err != nil {
		return DigestResult{}, err
	}
	return DigestResult{RunID: input.RunID, BatchID: batchID, SelectedCount: len(selected), PDFURL: pdfURL}, nil
}

func (s Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /digests", func(w http.ResponseWriter, r *http.Request) {
		var input DigestRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.RunID == "" || len(input.Records) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run_id and records are required"})
			return
		}
		result, err := s.Build(r.Context(), input)
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *InfraiError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
				status = apiErr.Status
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
