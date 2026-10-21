package digest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSelectReadyRecords(t *testing.T) {
	tests := []struct {
		name   string
		record MediaRecord
		want   bool
	}{
		{"processed video enters digest", MediaRecord{ProcessingJob: "completed", DurationSecs: 95}, true},
		{"active job waits", MediaRecord{ProcessingJob: "processing", DurationSecs: 95}, false},
		{"previously delivered stays out", MediaRecord{ProcessingJob: "completed", DurationSecs: 95, SummarySent: true}, false},
		{"empty media stays out", MediaRecord{ProcessingJob: "completed", DurationSecs: 0}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SelectReadyRecords([]MediaRecord{test.record})
			if (len(got) == 1) != test.want {
				t.Fatalf("selected=%v, want selected=%v", len(got) == 1, test.want)
			}
		})
	}
}

type recordingClient struct {
	submitted []MediaRecord
	markdown  string
}

func (c *recordingClient) Submit(_ context.Context, _ string, records []MediaRecord) (string, error) {
	c.submitted = records
	return "batch-42", nil
}
func (c *recordingClient) Wait(context.Context, string, time.Duration) error { return nil }
func (c *recordingClient) Results(context.Context, string) (map[string]string, error) {
	return map[string]string{"asset-7": "A concise episode summary."}, nil
}
func (c *recordingClient) GeneratePDF(_ context.Context, _ string, markdown string) (string, error) {
	c.markdown = markdown
	return "https://example.test/digest.pdf", nil
}

func TestBuildHandsBatchSummaryStraightToPDF(t *testing.T) {
	client := &recordingClient{}
	service := Service{Infrai: client, PollEvery: time.Millisecond}
	result, err := service.Build(context.Background(), DigestRequest{
		RunID: "2026-09-17",
		Records: []MediaRecord{
			{AssetID: "asset-7", CreatorID: "creator-2", Title: "Night Shift", Kind: "video", DurationSecs: 180, ProcessingJob: "completed"},
			{AssetID: "asset-8", CreatorID: "creator-2", Title: "Upload", Kind: "audio", DurationSecs: 30, ProcessingJob: "processing"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedCount != 1 || len(client.submitted) != 1 || client.submitted[0].AssetID != "asset-7" {
		t.Fatalf("unexpected selection: result=%+v submitted=%+v", result, client.submitted)
	}
	if !strings.Contains(client.markdown, "A concise episode summary.") || !strings.Contains(client.markdown, "creator-2") {
		t.Fatalf("PDF markdown did not receive creator summary: %q", client.markdown)
	}
}
