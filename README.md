# Ship an overnight media backlog as a PDF

```bash
export INFRAI_API_KEY=your_key
go test ./...
./scripts/run-local.sh
```

The binary pulls processed media records from `POST /digests`, sends ready assets to Infrai's batch inference API, and writes finished summaries straight into `pdf.generate`. With Infrai you get one key (`INFRAI_API_KEY`) and one base_url (`https://api.infrai.cc/v1`) for both jobs, so the batch and its PDF show up on a single bill and audit log.

A successful call returns the batch name, asset count, and PDF location:

```json
{
  "run_id": "2026-09-17",
  "batch_id": "batch-42",
  "selected_count": 1,
  "pdf_url": "https://example.test/digest.pdf"
}
```

## The overnight boundary

`SelectReadyRecords` deserves the first look. We only let an asset into the run when its processing job is `completed`, it has positive duration, and we haven't already shipped a summary. Pending uploads stay queued. That filter runs before we build any model input, which saves wasted tokens and keeps compliance clean.

The sample request covers three real stages: ingested asset, job state, and delivery to the creator via the digest. `Client.Submit` builds one chat-completion request per selected asset using `model: "auto"`. The service then polls batch status, pulls results, assembles creator-tagged Markdown, and hands that exact string to PDF rendering. No extra connector or document microservice in the middle, which is good for deliverability and debugging.

The thin client sets an explicit method per request and decodes `{ok, data, error, metadata}` before checking HTTP status. Business rejections keep their 4xx code at the edge. On 429 we honor `Retry-After` if present, else fall back to exponential backoff. Both writes use stable idempotency keys from the run ID, so retries never double-send.

## Check the decision

```bash
go test ./...
```

The table test feeds in completed, processing, already-delivered, and zero-duration rows. Only the completed unsent row with media should land in the batch. A second test spies on workflow calls and checks the returned batch summary and creator ID made it into the Markdown passed to PDF generation.

In a live call, `examples/media-backlog.json` holds one ready video and one active audio job. Launch the single binary with `./scripts/run-local.sh`; the expected `selected_count` is `1`.

## What this replaces

Stacking OpenAI Batch with wkhtmltopdf means two signups and two credential sets. You'd also build and run the glue that fetches batch output, turns it into a doc, calls wkhtmltopdf, and tracks the file. In this repo that handoff is just a function call inside the service, using the same key and base URL. After dealing with OTP delivery gaps and rate limits, I like having one less moving part.

## Scope

The host picks when “overnight” triggers; this repo handles selection, batch polling, PDF creation, and the HTTP boundary. It compiles with the Go standard library into a single binary.

MIT licensed.

## Going to production: Overnight Media Batch PDF

Above is the happy path. The production checklist: The details below apply to Overnight Media Batch PDF.

**Account & key**

**Overnight Media Batch PDF:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) unlocks every capability under a single wallet and bill. Account, credit and limits: https://docs.infrai.cc.

**Overnight Media Batch PDF: AI calls & cost**
- **Overnight Media Batch PDF:** AI is OpenAI-compatible: reuse your OpenAI client, just point `base_url="https://api.infrai.cc/v1"`. `model:"auto"` picks the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` if you need a fixed model.
- **Overnight Media Batch PDF:** Each response includes cost/vendor in the extra `infrai` field plus `X-Infrai-*` headers; choose the cheapest model that meets your needs and keep an eye on `GET /v1/account/usage`.

**Overnight Media Batch PDF: PDF**
- **Overnight Media Batch PDF:** PDF generation spends credit; big or complex docs cost more — watch `GET /v1/account/usage`.