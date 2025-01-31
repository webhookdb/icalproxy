# icalproxy

Solves the problem that basically no iCalendar (`.ics`) providers use proper HTTP headers,
which means to check for changes, your app has to download the entire request.

Exposes a root endpoint that is called like `/?url=<encoded icalendar url>`,
and either:

- Proxies the call to the requested URL if it's older than the configured TTL.
  - The TTL can be configured as per "Configuration" below.
- Serves the cached value.

All HTTP requests/responses adhere to useful HTTP headers:
- `HEAD` requests are honored.
- `Etag` headers are always served.
- `If-None-Match` headers are honored.
- `Last-Modified` headers are always served.
- `If-Modified-Since` headers are honored.

NOTE: While this project is focused on iCalendar feeds,
since their HTTP servers are particularly bad,
it can be used for any sort of feed or HTTP endpoint you want to add proper HTTP semantics to.

## Configuration

General purpose configuration:

- `PORT=18041`: Port serving http traffic.
- `DATABASE_URL=postgres://ical:ical@localhost:18042`: PostgreSQL URL used for storage.
  This application self-manages the database, you don't need to worry about migrations.
- `DATABASE_CONNECTION_POOL_URL=`: If provided, use this as the database connection string.
  icalproxy is compatible with transaction-mode connection pooling.
- `API_KEY=<value>`: Enable auth for the API. If set, one of two auth mechanics are required for protected endpoints:
  - `Authorization: Apikey <value>` header.
  - `Authorization: Basic :<value>` header. That is, basic auth where the password is the api key value,
    and the username is empty.
- `WEBHOOK_URL=`: The URL to POST to whenever a feed changes. See "Webhooks" below.
- `WEBHOOK_PAGE_SIZE=`: Number of URLs in each webhook request.
- `SENTRY_DSN=`: Set if using Sentry.

Feed contents are stored in object storage like AWS S3 or Cloudflare R2,
since otherwise they get too large for the relatively light needs of this database.

- `S3_ACCESS_KEY_ID=testkey`: AWS or similar access key (Cloudflare R2, etc).
  If empty, use the default AWS config loading behavior.
- `S3_ACCESS_KEY_SECRET=testsecret`: AWS or similar secret (R2, etc).
- `S3_BUCKET=icalproxy-feeds`: Bucket to store feeds.
- `S3_ENDPOINT=http://localhost:18043`: Endpoint to reach S3, R2, etc.
  Only set if not empty (so it can be empty for S3, for example).
  If using Cloudflare R2, set to `https://<account id>.r2.cloudflarestorage.com`
- `S3_PREFIX=icalproxy/feeds`: Key prefix to store feed files under.

Feed refresh configuration:

- `ICAL_BASE_TTL=2h`: The TTL for all `ics` feeds not overridden elsewhere in config.
  Specified as a [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration).
- `ICAL_TTL_ICLOUDCOM=5m`: The TTL for Apple iCloud iCalendar feeds.
  Specified as a [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration).

Any other host can also be configured to have specific durations.
This allows you to rapidly sync calendars from certain providers that serve `ics` feeds on-demand (like iCloud does),
or less often sync calendars from certain providers that have long `ics` feed cache times (like Google).

- For example, `ICAL_TTL_EXAMPLEORG=20m` would use a 20 minute TTL for all feeds hosted at `*.example.org`.
  The value after the `ICAL_TTL_` is compared against the URL host (case and punctuation independent).

Configuration for tuning and development:

- `DEBUG=false`: Enable debug logging and additional diagnostics.
- `LOG_FILE=`: Log to this filename.
- `LOG_FORMAT=`: One of `json`, `text`, or empty. If empty and in a TTY, use `text`, with color if possible.
  If empty and not in a TTY (so, running a real server), use `json`.
- `LOG_LEVEL=info`: Level to log at.
- `REQUEST_TIMEOUT=7`: When requesting an ICS url, and it is not in the database or has an expired TTL,
  a request is made synchronously. Because this is a slow, blocking request,
  it should have a fast timeout. If that request times out, the URL is still added
  to the database so it can be synced by the refresher, in case it's just a slow URL.
- `REQUEST_MAX_TIMEOUT=25`: When requesting an ICS url, and hitting the 'fallback' mode when the database is not available,
  use this timeout. Generally this should be a touch less than the load balancer timeout.
  We want to avoid load balancer timeouts since they indicate operations issues,
  whereas a timeout here is an origin issue.
- `REFRESH_PAGE_SIZE=100`: Number of feeds that are refreshed at a time before changes are committed to the database.
  Smaller pages will see more responsive updates, while larger pages may see better performance but more memory use.
- `REFRESH_TIMEOUT=30`: Seconds to wait for an origin server before timing out an ICalendar feed request.
  Only used for the refresh routine.

## Webhooks

If `WEBHOOK_URL` is set, whenever a row is modified, it will be marked for an update sent to `WEBHOOK_URL`.

- The body looks like `{"urls":[]}`
- The request is a `POST`.
- The timeout is 10 seconds.
- If the server replies back with a 2xx response, the rows are marked as notified about.
- Your server should request the updated URLs from the server;
  the webhook includes only the URLs, and no information about the contents.
- If `API_KEY` is set in icalproxy, then the webhook will include an `Authorization: Apikey <value>` header,
  which can be used for authentication on your server.
