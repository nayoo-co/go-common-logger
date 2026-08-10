package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

const (
	redactedHeaderValue = "[REDACTED]"
	unmatchedRoute      = "<unmatched>"
)

var safeHeaderNames = map[string]struct{}{
	"accept":            {},
	"accept-encoding":   {},
	"accept-language":   {},
	"cache-control":     {},
	"connection":        {},
	"content-encoding":  {},
	"content-language":  {},
	"content-length":    {},
	"content-type":      {},
	"date":              {},
	"expect":            {},
	"range":             {},
	"server":            {},
	"transfer-encoding": {},
	"upgrade":           {},
	"user-agent":        {},
	"vary":              {},
}

var sensitiveHeaderNameFragments = []string{
	"authorization",
	"cookie",
	"apikey",
	"token",
	"secret",
	"credential",
	"password",
	"session",
	"signature",
	"csrf",
	"xsrf",
	"key",
}

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

func LogTransaction(txn TransactionData) {
	txn = sanitizedTransaction(txn)
	logTransaction(txn)
}

func logTransaction(txn TransactionData) {
	jsonBytes, _ := json.Marshal(txn)
	fmt.Println(string(jsonBytes))
}

func LogDebug(dbg DebugData) {
	jsonBytes, _ := json.Marshal(dbg)
	fmt.Fprintln(os.Stderr, string(jsonBytes))
}

func LogDebugMessage(traceID, componentName, message, status, level string) {

	// Get caller filename and line number (skip 2 levels: LogDebugMessage -> LogDebugFromContext -> actual caller)
	_, filename, line, _ := runtime.Caller(2)
	filename = filepath.Base(filename)

	LogDebug(DebugData{
		TraceID:     traceID,
		Level:       level,
		Line:        line,
		Filename:    filename,
		Hostname:    getHostname(),
		LoggingTime: time.Now(),
		Status:      status,
		Message:     message,
		Application: componentName,
	})
}

func LoggingMiddleware(componentName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		span := trace.SpanFromContext(c.Request.Context())
		traceID := span.SpanContext().TraceID().String()
		spanID := span.SpanContext().SpanID().String()

		startTime := time.Now()

		c.Next()

		endTime := time.Now()
		responseSize := c.Writer.Size()
		if responseSize < 0 {
			responseSize = 0
		}

		txn := TransactionData{
			Status:         c.Writer.Status(),
			Start:          startTime,
			End:            endTime,
			RequestBody:    "",
			RequestHeader:  sanitizeHeaders(c.Request.Header),
			ResponseBody:   "",
			ResponseHeader: sanitizeHeaders(c.Writer.Header()),
			TraceID:        traceID,
			SpanID:         spanID,
			Duration:       int(endTime.Sub(startTime).Milliseconds()),
			RequestMethod:  c.Request.Method,
			Hostname:       getHostname(),
			LoggingTime:    endTime,
			Level:          "INFO",
			Application:    componentName,
			ApiUrl:         requestRoute(c),
			Size:           responseSize,
		}

		logTransaction(txn)
	}
}

func requestRoute(c *gin.Context) string {
	route := c.FullPath()
	if route == "" {
		return unmatchedRoute
	}
	return route
}

func sanitizedTransaction(txn TransactionData) TransactionData {
	txn.RequestBody = ""
	txn.RequestHeader = sanitizeHeaders(txn.RequestHeader)
	txn.ResponseBody = ""
	txn.ResponseHeader = sanitizeHeaders(txn.ResponseHeader)
	return txn
}

func sanitizeHeaders(headers map[string][]string) map[string][]string {
	sanitized := make(map[string][]string)

	for name, values := range headers {
		normalizedName := strings.ToLower(name)
		if isSensitiveHeaderName(normalizedName) {
			sanitized[name] = []string{redactedHeaderValue}
			continue
		}

		if _, safe := safeHeaderNames[normalizedName]; safe {
			sanitized[name] = append([]string{}, values...)
		}
	}

	return sanitized
}

func isSensitiveHeaderName(normalizedName string) bool {
	compactName := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, normalizedName)

	for _, fragment := range sensitiveHeaderNameFragments {
		if strings.Contains(compactName, fragment) {
			return true
		}
	}

	return false
}

type ResponseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *ResponseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

var (
	resolveHostOnce sync.Once
	cachedHostname  string
)

func getHostname() string {
	resolveHostOnce.Do(func() {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown"
		}
		cachedHostname = host
	})
	return cachedHostname
}
