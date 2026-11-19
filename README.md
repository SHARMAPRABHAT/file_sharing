# Secure PDF Paywall (MVP) — Go + Gin + Razorpay/UPI + Google Drive/Sheets

A lightweight MVP to **sell PDFs via a shareable link**. Users open a link, complete payment (Razorpay/UPI), and then receive **time-limited access** to the PDF. Built as a **monolithic Go app** using **Gin**, with a simple **HTML frontend** (no React).

> ✅ Goal: **Take payment → deliver PDF securely → token expires**  
> ✅ Low-cost MVP: uses **Google Drive** for file storage and **Google Sheets** as a simple database alternative.

---

## Features

- **Pay-to-access PDF** using a public share link (e.g., `/buy/:productId`)
- **Razorpay Checkout** (supports UPI, Cards, NetBanking etc. depending on Razorpay setup)
- **Payment verification** on backend
- **Short-lived access token** after successful payment (e.g., 30 minutes)
- **Secure delivery**
  - Stream PDF via backend (recommended)
  - OR provide a temporary download link
- **Google Drive** for storing PDFs privately
- **Google Sheets** as a lightweight datastore for:
  - Product metadata (PDF Drive file id, title, price)
  - Payment logs
  - Token issuance + expiry

---

## Tech Stack

- **Backend**: Go (Golang), Gin
- **Frontend**: Plain HTML/CSS/JS (server-rendered or static)
- **Payments**: Razorpay Checkout + server-side verification
- **Storage**: Google Drive (private files)
- **Datastore**: Google Sheets (via API)
- **Hosting**: Any free/low-cost host that supports Go (Render/Fly.io/Railway etc.)

---

## High-Level Flow

1. User opens a link like:  
   `https://your-domain.com/buy/<productId>`

2. User clicks **Pay Now** → Razorpay Checkout opens.

3. After payment:
   - Razorpay returns `payment_id`, `order_id`, `signature` (or equivalent)
   - Backend verifies the payment & signature
   - Backend issues a **short-lived token**

4. User is redirected to:  
   `https://your-domain.com/access/<token>`

5. Backend validates token expiry and streams the PDF securely.

---

## Project Structure (Suggested)

```text
secure-pdf-paywall/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── handlers/
│   │   ├── buy.go
│   │   ├── payment.go
│   │   └── access.go
│   ├── services/
│   │   ├── razorpay.go
│   │   ├── token.go
│   │   ├── drive.go
│   │   └── sheets.go
│   ├── middleware/
│   │   └── security.go
│   └── models/
│       └── models.go
├── web/
│   ├── static/
│   │   ├── css/
│   │   └── js/
│   └── templates/
│       ├── buy.html
│       ├── success.html
│       └── error.html
├── .env.example
├── go.mod
└── README.md
```

---

## Setup

### 1) Prerequisites

- Go 1.20+ recommended
- Razorpay account (test/live keys)
- Google Cloud project with enabled APIs:
  - Google Drive API
  - Google Sheets API
- Service account credentials JSON

---

### 2) Environment Variables

Create a `.env` file based on `.env.example`.

#### `.env.example`

```env
# Server
APP_PORT=8080
APP_BASE_URL=http://localhost:8080

# Razorpay (Test keys)
RAZORPAY_KEY_ID=rzp_test_xxxxx
RAZORPAY_KEY_SECRET=xxxxx

# Token settings
TOKEN_TTL_MINUTES=30
TOKEN_SECRET=super-secret-hmac-key

# Google
GOOGLE_SERVICE_ACCOUNT_JSON=./secrets/service-account.json
GOOGLE_SHEET_ID=xxxxxxxxxxxxxxxxxxxxxxxxxxxx
GOOGLE_CATEGORY_SHEET_ID=1M8UoN1uuDX_m2U1o3GVAAQCYKqxTsoJtbgZbbgH5LGU
GOOGLE_COURSE_SHEET_ID=1M8UoN1uuDX_m2U1o3GVAAQCYKqxTsoJtbgZbbgH5LGU
GOOGLE_DRIVE_FOLDER_ID=xxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Optional
LOG_LEVEL=info
```

> ✅ Keep `.env` and service account JSON **out of git** (add to `.gitignore`).

---

## Google Sheets Format (Recommended)

Buy-note categories load from the sheet identified by `GOOGLE_CATEGORY_SHEET_ID`
(defaults to the category spreadsheet above), tab gid `548445004`. The tab must
include `Category ID`, `Title`, `Description`, and `Status` columns; only rows
with status `Enabled` are returned by `/api/categories`. Category data refreshes
on the `SHEET_REFRESH_MINUTES` interval.

The `/buy-courses` catalog loads entirely from `GOOGLE_COURSE_SHEET_ID`
(defaults to the same spreadsheet), tab gid `1320525194`. Add one row per course
using these columns: `pack`, `title`, `price`, `discount_price`,
`early_bird_price`, `offer_price`, `discount_percent`, `sold`, `total`,
`offer_title`, `description`, `guide_url`, `google_drive_id`, and `Status`.
Rows marked `Enable` or `Enabled` appear on the page in sheet order. Catalog,
sales, guide, and paid delivery data refresh from the sheet every
`SHEET_REFRESH_MINUTES` interval.

Create a Google Sheet with these tabs:

### `products` tab

Columns:
- `product_id`
- `title`
- `price_inr`
- `drive_file_id`
- `active`

### `payments` tab

Columns:
- `created_at`
- `product_id`
- `mobile`
- `razorpay_payment_id`
- `status`

### `tokens` tab

Columns:
- `token`
- `product_id`
- `created_at`
- `expires_at`
- `redeemed`

---

## Run Locally

```bash
go mod tidy
go run ./cmd/server
```

Open:
- `http://localhost:8080/buy/<category>/<productId>`

---

## Endpoints (Example)

- `GET /buy/:category/:productId`
  - Renders buy page and loads Razorpay Checkout config.
- `GET /buy/:productId`
  - Backward-compatible route that also renders the buy page.

- `POST /api/create-order`
  - Creates Razorpay order from backend

- `POST /api/verify-payment`
  - Verifies payment signature
  - Issues token

- `GET /access/:token`
  - Validates token and streams PDF

---

## Security Notes (Important)

- Do **NOT** expose direct Google Drive public links.
- Keep Drive files private; backend should fetch using service account.
- Always verify payment on backend (signature/transaction check).
- Tokens must be:
  - random
  - time-limited
  - single-use (optional)
- Add basic abuse protection:
  - rate-limit `/access/:token`
  - log token checks
  - optionally lock token to mobile number + IP (advanced)

---

## MVP Scope vs Future Scope

### MVP (Now)
- Single owner uploads PDFs (manual upload to Drive)
- Product metadata stored in Google Sheet
- Simple payment flow + token access
- Monolithic Go repo + plain HTML pages

### Future (SaaS / LMS)
- Multi-tenant: many sellers upload their own files
- Real database (Postgres)
- User accounts, dashboards, analytics
- Storage abstraction (S3/Cloud Storage)
- Webhooks for robust payment status updates

---

## Contributing

- Fork the repo
- Create a feature branch
- Open a PR with a clear description

---

## License

This project is provided as-is for MVP/learning purposes.  
Add your preferred license (MIT/Apache-2.0) before public release.

---

## Next Steps (Recommended)

- [ ] Add Razorpay **webhook** support for reliable confirmation
- [ ] Implement **streaming** (no direct download URLs)
- [ ] Add **single-use** tokens
- [ ] Add **rate limiting**
- [ ] Deploy to a free host and test full flow in public
