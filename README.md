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
- `API_KEY=<value>`: Enable auth for the API. If set,
  all `/` requests require an `Authorization: Apikey <value>` header.

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
- `REFRESH_PAGE_SIZE=100`: Number of feeds that are refreshed at a time before changes are committed to the database.
  Smaller pages will see more responsive updates, while larger pages may see better performance.
- `REFRESH_TIMEOUT=60`: How long to wait for an origin server before timing out an ICalendar feed request.
  Only used for the refresh routine.