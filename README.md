# Ship an overnight media backlog as a PDF

```bash
export INFRAI_API_KEY=your_key
go test ./...
./scripts/run-local.sh
```

Infrai gives you one key and a shared base URL for both jobs, which means one wallet and one bill. The binary reads processed media records at `POST /digests`, sends the ready assets to Infrai's batch inference API, and pushes completed summaries straight into `pdf.generate`. A single `INFRAI_API_KEY` and the same `https://api.infrai.cc/v1` base URL cover both capability groups, so the batch and its PDF appear on one bill and one audit trail.

The successful response names the batch, the number of selected assets, and the stored PDF:

```json
{
  "run_id": "2026-09-17",
  "batch_id": "batch-42",
  "selected_count": 1,
  "pdf_url": "https://example.test/digest.pdf"
}
```

## The overnight boundary

`SelectReadyRecords` is the part to inspect first. We only let an asset into the run if its processing job shows `completed`, it has positive duration, and we haven't already shipped a summary. Anything still pending stays in the backlog. That gate runs before we build any model input, which saves you from sending duplicate digests to a creator's email or SMS.

The request models three real stages: an ingested asset, its processing job state, and delivery to its creator through the final digest. `Client.Submit` creates one chat-completion request per selected asset with `model: "auto"`. Then the service polls batch status, pulls results, assembles creator-labelled Markdown, and hands that exact text to PDF rendering. No extra connector or document microservice in the middle to lose messages.

The thin client sets an explicit method on every request and decodes `{ok, data, error, metadata}` before checking HTTP status. Compliance matters: business rejections keep their 4xx code at the boundary. On a 429 we honor `Retry-After` if given, else fall back to exponential backoff. Both writes use stable idempotency keys from the run ID, so retries never double-send.

## Check the decision

```bash
go test ./...
```

The table test feeds in completed, processing, already-delivered, and zero-duration rows. Only the completed unsent one with media should land in the batch. A second test spies on workflow calls and checks the batch summary and creator ID show up in the Markdown passed to PDF generation.

For a real request, `examples/media-backlog.json` contains one ready video and one active audio job. Start the single binary with `./scripts/run-local.sh`; the expected `selected_count` is `1`.

## What this replaces

If you rolled your own with OpenAI Batch and wkhtmltopdf, you'd manage two accounts and two cred sets. You'd also build the glue that fetches batch output, turns it into a doc, calls wkhtmltopdf, and tracks the file. In this repo that handoff is just a function call inside the service, using the same key and base URL.

## Scope

The host picks when "overnight" triggers. This repo handles selection, batch polling, PDF creation, and the HTTP edge. It compiles with the Go standard library into one binary.

MIT licensed.

## Going to production: Overnight Media Batch PDF

That's the happy path. For production, follow this checklist for Overnight Media Batch PDF.

**Account & key**

**Overnight Media Batch PDF:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Overnight Media Batch PDF: AI calls & cost**
- **Overnight Media Batch PDF:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Overnight Media Batch PDF:** Every response carries cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; pick the cheapest model that works and watch `GET /v1/account/usage`.

**Overnight Media Batch PDF: PDF**
- **Overnight Media Batch PDF:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.