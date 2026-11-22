# INF Works: a knowledge base prototype

A working prototype of the **Stage 1 specification** (`Stage.pdf`) for the INF/UFRGS academic works knowledge base. It implements both functionalities:

- **F1: Search and discovery** (public, no login). Search by title or keyword, filter by author, advisor, research area, year and type (all combined with AND). Results come 10 per page in a stable order. A details page shows the metadata, the PDF or external link (or "Full text unavailable"), contact info only when it is public, and up to five similar works ranked by keyword overlap.
- **F2: Register and update** (library workers), with **moderation** (coordinators). Workers save incomplete drafts, then submit complete records. Submission validates the required fields, checks the document source (PDF or http(s) URL), and blocks duplicates on normalized title + year + first author. Coordinators approve, or reject with a mandatory reason. Rejected records can be corrected and resubmitted. Edits to published records are audited.

## Run

Requires Go 1.22+ (no cgo; SQLite is the pure-Go `modernc.org/sqlite`).

```sh
go run .                     # http://localhost:8080, data in ./data
go run . -addr :9000 -data /tmp/infworks -no-seed
go test ./...                # 47 tests, named after the spec's TS-xx / TW-xx cases
```

On first start the database is seeded with **fictional** demo data: 24 published works, plus one draft, one record under review and one rejected record. It also creates these mock accounts (password = username):

| User | Role | Can |
|---|---|---|
| `library` | LIBRARY_WORKER | create, edit, save drafts, submit |
| `coordinator`, `coordinator2` | COORDINATOR | approve or reject the queue |
| `researcher` | RESEARCHER | nothing beyond public search (gets 403 on writes) |

To reset, delete the `data/` directory.

## Structure (mirrors the package diagram, Fig. 14)

| Layer | Package | Contents |
|---|---|---|
| Presentation | `internal/web` | HTTP handlers, sessions, `html/template` pages, CSS |
| Application | `internal/app` | `Search`, `Cataloguing`, `Moderation` services; `Repository` and `DocumentStore` ports |
| Domain | `internal/domain` | `Work`, `WorkType`, `Status`, roles, lifecycle table (Table 6), validation, normalization |
| Infrastructure | `internal/store` | SQLite repository, PDF file storage, mock identity, seed data |

Authorization is enforced in the application services, not just in the UI (NFR-03), so direct HTTP requests from other roles get 401 or 403.

## Decisions taken where the spec left values open (Section 9.1)

| Open item | Prototype choice |
|---|---|
| Free-text max length | 200 characters (per AC-01d), checked in the browser and on the server |
| Year range | 1950 to the current year, four digits |
| File size limit | 20 MB; PDF verified by `.pdf` extension, `%PDF-` magic bytes and sniffed MIME type |
| URL allowlist | any well-formed absolute `http`/`https` URL |
| Duplicate normalization | lowercase, collapse whitespace, strip common Latin diacritics; checked against non-draft records |
| Multiple advisors | single primary advisor (as the MVP states) |
| Keyword/area count | at least one, no upper limit; comma-separated, de-duplicated |

Other interpretation choices:

- **UNDER_REVIEW records are locked** for editing until a coordinator decides.
- **REJECTED records can be saved without resubmitting.** They stay REJECTED until resubmitted.
- **Edits to published records must keep the mandatory metadata complete.** They stay PUBLISHED, with an audit entry (D5, provisional policy).
- **Optimistic locking** (a record `version`) blocks stale edits and a second coordinator's stale decision (TW-25).
- **Submission is atomic.** The PDF is stored first and removed if the database write fails, so nothing reaches the queue (TW-26, NFR-06).
- **Every lifecycle action is audited**, not just published edits.

## Not included (out of scope, or mocked)

The real UFRGS authentication (mocked by local demo users), notifications, author correction requests, statistics, citation graph and AI features. The NFR-02 performance target and broader mobile testing still need a proper evaluation.
