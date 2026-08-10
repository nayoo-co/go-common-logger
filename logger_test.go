package logger

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func TestSanitizeHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string][]string
		want    map[string][]string
	}{
		{
			name:    "nil headers",
			headers: nil,
			want:    map[string][]string{},
		},
		{
			name:    "empty headers",
			headers: map[string][]string{},
			want:    map[string][]string{},
		},
		{
			name: "safe headers preserve case and multiple values",
			headers: map[string][]string{
				"aCcEpT":       {"application/json", "text/plain"},
				"Content-Type": {"application/json"},
			},
			want: map[string][]string{
				"aCcEpT":       {"application/json", "text/plain"},
				"Content-Type": {"application/json"},
			},
		},
		{
			name: "sensitive headers are case insensitive and redacted",
			headers: map[string][]string{
				"aUtHoRiZaTiOn": {"Bearer authorization-secret"},
				"COOKIE":        {"cookie-secret=1"},
				"aPiKeY":        {"apikey-secret"},
				"X-API-KEY":     {"x-api-key-secret"},
				"X-Internal-Token": {
					"internal-token-secret",
				},
				"X-Refresh-Token": {"refresh-token-secret"},
				"X-Auth-Token":    {"auth-token-secret"},
				"X-CSRF-Token":    {"csrf-secret"},
				"X-XSRF-Token":    {"xsrf-secret"},
			},
			want: map[string][]string{
				"aUtHoRiZaTiOn":    {redactedHeaderValue},
				"COOKIE":           {redactedHeaderValue},
				"aPiKeY":           {redactedHeaderValue},
				"X-API-KEY":        {redactedHeaderValue},
				"X-Internal-Token": {redactedHeaderValue},
				"X-Refresh-Token":  {redactedHeaderValue},
				"X-Auth-Token":     {redactedHeaderValue},
				"X-CSRF-Token":     {redactedHeaderValue},
				"X-XSRF-Token":     {redactedHeaderValue},
			},
		},
		{
			name: "generic sensitive categories are redacted and unknown headers omitted",
			headers: map[string][]string{
				"X-Client-Secret":       {"client-secret"},
				"X-Database-Credential": {"database-credential"},
				"X-User-Password":       {"user-password"},
				"X-Login-Session":       {"login-session"},
				"X-Request-Signature":   {"request-signature"},
				"X-Signing-Key":         {"signing-key"},
				"X-Custom-Metadata":     {"unknown-value"},
			},
			want: map[string][]string{
				"X-Client-Secret":       {redactedHeaderValue},
				"X-Database-Credential": {redactedHeaderValue},
				"X-User-Password":       {redactedHeaderValue},
				"X-Login-Session":       {redactedHeaderValue},
				"X-Request-Signature":   {redactedHeaderValue},
				"X-Signing-Key":         {redactedHeaderValue},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := cloneHeaders(tt.headers)
			got := sanitizeHeaders(tt.headers)

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("sanitizeHeaders() = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.headers, original) {
				t.Fatalf("sanitizeHeaders() mutated input: got %#v, want %#v", tt.headers, original)
			}
		})
	}
}

func TestSanitizeHeaders_ReturnsIndependentValues(t *testing.T) {
	original := map[string][]string{
		"Accept": {"application/json", "text/plain"},
	}

	sanitized := sanitizeHeaders(original)
	sanitized["Accept"][0] = "mutated"
	sanitized["Content-Type"] = []string{"text/html"}

	want := map[string][]string{
		"Accept": {"application/json", "text/plain"},
	}
	if !reflect.DeepEqual(original, want) {
		t.Fatalf("mutating sanitized headers changed input: got %#v, want %#v", original, want)
	}
}

func TestLogTransaction_SanitizesCallerDataWithoutMutation(t *testing.T) {
	txn := TransactionData{
		RequestBody: "manual-request-body-secret",
		RequestHeader: map[string][]string{
			"Authorization": {"manual-authorization-secret"},
			"Content-Type":  {"application/json"},
		},
		ResponseBody: "manual-response-body-secret",
		ResponseHeader: map[string][]string{
			"Set-Cookie": {"manual-cookie-secret"},
		},
	}
	original := txn
	original.RequestHeader = cloneHeaders(txn.RequestHeader)
	original.ResponseHeader = cloneHeaders(txn.ResponseHeader)

	output := captureStdout(t, func() {
		LogTransaction(txn)
	})

	for _, secret := range []string{
		"manual-request-body-secret",
		"manual-authorization-secret",
		"manual-response-body-secret",
		"manual-cookie-secret",
	} {
		if strings.Contains(output, secret) {
			t.Errorf("transaction output contains secret %q", secret)
		}
	}
	if !reflect.DeepEqual(txn, original) {
		t.Fatalf("LogTransaction() mutated input: got %#v, want %#v", txn, original)
	}

	var logged TransactionData
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logged); err != nil {
		t.Fatalf("unmarshal transaction output: %v\noutput: %s", err, output)
	}
	if logged.RequestBody != "" || logged.ResponseBody != "" {
		t.Fatalf("bodies were not suppressed: request=%q response=%q", logged.RequestBody, logged.ResponseBody)
	}
	if got := logged.RequestHeader["Authorization"]; !reflect.DeepEqual(got, []string{redactedHeaderValue}) {
		t.Errorf("authorization header = %#v, want redacted", got)
	}
	if got := logged.RequestHeader["Content-Type"]; !reflect.DeepEqual(got, []string{"application/json"}) {
		t.Errorf("content type = %#v, want preserved value", got)
	}
}

func TestLoggingMiddleware_SuppressesBodiesAndSensitiveHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(LoggingMiddleware("test-component"))
	router.POST("/transactions/:transactionID", func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Header("X-Response-Secret", "response-header-secret")
		c.Header("X-Custom-Metadata", "response-unknown-secret")
		c.String(http.StatusCreated, "response-body-secret")
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/transactions/dynamic-path-secret?apikey=query-secret",
		strings.NewReader("request-body-secret"),
	)
	request.Header["aPiKeY"] = []string{"request-apikey-secret", "second-apikey-secret"}
	request.Header["CoNtEnT-TyPe"] = []string{"application/json"}
	request.Header["X-Custom-Metadata"] = []string{"request-unknown-secret"}
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
		Remote:  true,
	})
	request = request.WithContext(trace.ContextWithSpanContext(request.Context(), spanContext))
	originalRequestHeaders := cloneHeaders(request.Header)
	recorder := httptest.NewRecorder()

	output := captureStdout(t, func() {
		router.ServeHTTP(recorder, request)
	})

	for _, secret := range []string{
		"request-body-secret",
		"response-body-secret",
		"request-apikey-secret",
		"second-apikey-secret",
		"response-header-secret",
		"request-unknown-secret",
		"response-unknown-secret",
		"dynamic-path-secret",
		"query-secret",
	} {
		if strings.Contains(output, secret) {
			t.Errorf("middleware output contains secret %q", secret)
		}
	}
	if !reflect.DeepEqual(map[string][]string(request.Header), originalRequestHeaders) {
		t.Fatalf("middleware mutated request headers: got %#v, want %#v", request.Header, originalRequestHeaders)
	}

	var txn TransactionData
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &txn); err != nil {
		t.Fatalf("unmarshal transaction output: %v\noutput: %s", err, output)
	}
	if txn.RequestBody != "" || txn.ResponseBody != "" {
		t.Fatalf("bodies were not suppressed: request=%q response=%q", txn.RequestBody, txn.ResponseBody)
	}
	if txn.Status != http.StatusCreated {
		t.Errorf("Status = %d, want %d", txn.Status, http.StatusCreated)
	}
	if txn.Size != len("response-body-secret") {
		t.Errorf("Size = %d, want %d", txn.Size, len("response-body-secret"))
	}
	if txn.ApiUrl != "/transactions/:transactionID" {
		t.Errorf("ApiUrl = %q, want route template", txn.ApiUrl)
	}
	if txn.TraceID != traceID.String() || txn.SpanID != spanID.String() {
		t.Errorf(
			"trace metadata = (%q, %q), want (%q, %q)",
			txn.TraceID,
			txn.SpanID,
			traceID.String(),
			spanID.String(),
		)
	}
	if got := txn.RequestHeader["aPiKeY"]; !reflect.DeepEqual(got, []string{redactedHeaderValue}) {
		t.Errorf("request API key = %#v, want redacted", got)
	}
	if got := txn.RequestHeader["CoNtEnT-TyPe"]; !reflect.DeepEqual(got, []string{"application/json"}) {
		t.Errorf("request content type = %#v, want preserved value", got)
	}
	if got := txn.ResponseHeader["X-Response-Secret"]; !reflect.DeepEqual(got, []string{redactedHeaderValue}) {
		t.Errorf("response secret = %#v, want redacted", got)
	}
}

func TestLoggingMiddleware_UsesFallbackForUnmatchedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(LoggingMiddleware("test-component"))
	request := httptest.NewRequest(
		http.MethodGet,
		"/unmatched/dynamic-path-secret?token=query-secret",
		nil,
	)
	recorder := httptest.NewRecorder()

	output := captureStdout(t, func() {
		router.ServeHTTP(recorder, request)
	})

	if strings.Contains(output, "dynamic-path-secret") || strings.Contains(output, "query-secret") {
		t.Fatalf("unmatched-route output contains request data: %s", output)
	}

	var txn TransactionData
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &txn); err != nil {
		t.Fatalf("unmarshal transaction output: %v\noutput: %s", err, output)
	}
	if txn.ApiUrl != unmatchedRoute {
		t.Errorf("ApiUrl = %q, want %q", txn.ApiUrl, unmatchedRoute)
	}
	if txn.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", txn.Status, http.StatusNotFound)
	}
	if txn.Size != 0 {
		t.Errorf("Size = %d, want 0", txn.Size)
	}
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if headers == nil {
		return nil
	}

	cloned := make(map[string][]string, len(headers))
	for name, values := range headers {
		cloned[name] = append([]string{}, values...)
	}
	return cloned
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	originalStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = reader.Close()
		_ = writer.Close()
	})

	fn()

	os.Stdout = originalStdout
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	return string(output)
}
