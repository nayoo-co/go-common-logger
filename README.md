# go-common-logger

A Go package for common logging, specifically for the Gin framework with OpenTelemetry tracing support.

## Features

- Log transaction metadata for HTTP requests and responses
- Log debug logs with trace ID from OpenTelemetry
- Middleware for Gin that automatically logs request/response data
- Supports JSON format for logs

## Installation

```bash
go get github.com/serpentdark/go-common-logger
```

## Usage

### Importing the package

```go
import "github.com/serpentdark/go-common-logger/logger"
```

### Using LoggingMiddleware in Gin

```go
r := gin.New()
r.Use(logger.LoggingMiddleware("my-app"))
```

This middleware will log transaction data for every request, including:
- A conservative allowlist of non-sensitive request/response headers
- Trace ID and Span ID from OpenTelemetry
- Duration
- Status code
- And more

Request and response bodies are suppressed. Credential-bearing header names are retained with
`[REDACTED]` values, and headers outside the safe allowlist are omitted. Header maps passed to the
logger are never mutated. `ApiUrl` contains the matched route template without query parameters or
dynamic path values; unmatched requests use the stable value `<unmatched>`.

### Logging Debug Messages

```go
logger.LogDebugMessage(ctx, "my-component", "This is a debug message", "success", "DEBUG")
```

This function will log a debug message with data:
- Trace ID from context
- Filename and line number of the caller
- Hostname
- Time
- Message
- And more

### Logging Transaction Data Manually

```go
txn := logger.TransactionData{
    Status: 200,
    // ... fill in other fields
}
logger.LogTransaction(txn)
```

The same body suppression and header sanitization defaults apply to manually logged transactions.

### Logging Debug Data Manually

```go
dbg := logger.DebugData{
    Level: "INFO",
    Message: "Custom debug message",
    // ... fill in other fields
}
logger.LogDebug(dbg)
```

## Data Structures

### TransactionData

```go
type TransactionData struct {
    Status         int                 `json:"ResponseCode"`
    Start          time.Time           `json:"Start"`
    End            time.Time           `json:"End"`
    RequestBody    string              `json:"RequestBody"`
    RequestHeader  map[string][]string `json:"RequestHeader"`
    ResponseBody   string              `json:"ResponseBody"`
    ResponseHeader map[string][]string `json:"ResponseHeader"`
    TraceID        string              `json:"trace_id"`
    SpanID         string              `json:"span_id"`
    Duration       int                 `json:"Duration"`
    RequestMethod  string              `json:"RequestMethod"`
    Hostname       string              `json:"Hostname"`
    LoggingTime    time.Time           `json:"LoggingTime"`
    Level          string              `json:"Level"`
    Application    string              `json:"Application"`
    ApiUrl         string              `json:"ApiUrl"`
    Size           int                 `json:"Size"`
}
```

### DebugData

```go
type DebugData struct {
    TraceID     string    `json:"trace_id"`
    Level       string    `json:"Level"`
    Line        int       `json:"Line"`
    Filename    string    `json:"Filename"`
    Hostname    string    `json:"Hostname"`
    LoggingTime time.Time `json:"LoggingTime"`
    Status      string    `json:"Status"`
    Message     string    `json:"Message"`
    Application string    `json:"Application"`
}
```

## Dependencies

- [gin-gonic/gin](https://github.com/gin-gonic/gin) - Gin web framework
- [go.opentelemetry.io/otel/trace](https://github.com/open-telemetry/opentelemetry-go) - OpenTelemetry tracing

## Example Project

See the code in `logger.go` for full implementation.

## License

This project is under the MIT License
