package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

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
	jsonBytes, _ := json.Marshal(txn)
	fmt.Println(string(jsonBytes))
}

func LogDebug(dbg DebugData) {
	jsonBytes, _ := json.Marshal(dbg)
	fmt.Fprintln(os.Stderr, string(jsonBytes))
}

func LogDebugMessage(ctx context.Context, componentName, message, status, level string) {
	// Extract traceID from context like LoggingMiddleware does
	span := trace.SpanFromContext(ctx)
	traceID := span.SpanContext().TraceID().String()

	// Get caller filename and line number (skip 2 levels: LogDebugMessage -> LogDebugFromContext -> actual caller)
	_, filename, line, _ := runtime.Caller(2)
	filename = filepath.Base(filename)

	LogDebug(DebugData{
		TraceID:     traceID,
		Level:       level,
		Line:        line,
		Filename:    filename,
		Hostname:    getHostname(),
		LoggingTime: time.Now().UTC(),
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

		startTime := time.Now().UTC()

		var requestBody []byte
		if c.Request.Body != nil {
			requestBody, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		writer := &ResponseWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = writer

		c.Next()

		endTime := time.Now().UTC()

		txn := TransactionData{
			Status:         c.Writer.Status(),
			Start:          startTime,
			End:            endTime,
			RequestBody:    string(requestBody),
			RequestHeader:  c.Request.Header,
			ResponseBody:   writer.body.String(),
			ResponseHeader: writer.ResponseWriter.Header(),
			TraceID:        traceID,
			SpanID:         spanID,
			Duration:       int(endTime.Sub(startTime).Milliseconds()),
			RequestMethod:  c.Request.Method,
			Hostname:       getHostname(),
			LoggingTime:    endTime,
			Level:          "INFO",
			Application:    componentName,
			ApiUrl:         c.Request.RequestURI,
			Size:           writer.body.Len(),
		}

		LogTransaction(txn)
	}
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
