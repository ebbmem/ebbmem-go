// Copyright 2026 Spectrayan — Proprietary

// Package ebbmem is the Ebb client SDK.
// Proprietary, (c) Spectrayan.
package ebbmem

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ebbmem/ebbmem-go/internal/generated"
)

const SDKVersion = "0.2.0"

// Client is the facade over the generated Go API client.
type Client struct {
	baseURL    string
	token      string
	apiKey     string
	namespace  string
	maxRetries int
	http       *http.Client
	cfg        *generated.Configuration
	raw        *generated.APIClient
	Memory     Memory
	Tasks      Tasks
	Events     Events
}

// Raw returns the generated API client.
func (c *Client) Raw() *generated.APIClient { return c.raw }

// Option configures NewClient. Explicit options win over the environment.
type Option func(*settings)

type settings struct {
	baseURL    *string
	token      *string
	apiKey     *string
	namespace  *string
	timeout    *time.Duration
	maxRetries *int
	headers    map[string]string
	userAgent  *string
}

func WithBaseURL(value string) Option {
	return func(s *settings) { s.baseURL = &value }
}
func WithToken(value string) Option {
	return func(s *settings) { s.token = &value }
}
func WithAPIKey(value string) Option {
	return func(s *settings) { s.apiKey = &value }
}
func WithNamespace(value string) Option {
	return func(s *settings) { s.namespace = &value }
}
func WithTimeout(value time.Duration) Option {
	return func(s *settings) { s.timeout = &value }
}
func WithMaxRetries(value int) Option {
	return func(s *settings) { s.maxRetries = &value }
}
func WithHeader(name, value string) Option {
	return func(s *settings) {
		if s.headers == nil {
			s.headers = map[string]string{}
		}
		s.headers[name] = value
	}
}
func WithUserAgent(value string) Option {
	return func(s *settings) { s.userAgent = &value }
}

// NewClient builds a client. base URL is required (argument or EBBMEM_ORIGIN).
func NewClient(opts ...Option) (*Client, error) {
	var s settings
	for _, opt := range opts {
		opt(&s)
	}
	base := env("EBBMEM_ORIGIN")
	if s.baseURL != nil {
		base = *s.baseURL
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("base URL is required (set it or EBBMEM_ORIGIN)")
	}
	token := env("EBBMEM_TOKEN")
	if s.token != nil {
		token = *s.token
	}
	apiKey := env("EBBMEM_API_KEY")
	if s.apiKey != nil {
		apiKey = *s.apiKey
	}
	namespace := env("EBBMEM_NAMESPACE")
	if s.namespace != nil {
		namespace = *s.namespace
	}
	timeout := 30 * time.Second
	if s.timeout != nil {
		timeout = *s.timeout
	}
	retries := 2
	if s.maxRetries != nil {
		retries = *s.maxRetries
	}
	ua := "ebbmem-client-go/" + SDKVersion
	if s.userAgent != nil && *s.userAgent != "" {
		ua = *s.userAgent
	}
	cfg := generated.NewConfiguration()
	origin := cfg.Servers[0].Variables["origin"]
	origin.DefaultValue = base
	cfg.Servers[0].Variables["origin"] = origin
	cfg.UserAgent = ua
	cfg.HTTPClient = &http.Client{Timeout: timeout}
	cfg.Debug = false
	for k, v := range s.headers {
		cfg.AddDefaultHeader(k, v)
	}
	client := &Client{
		baseURL: base, token: token, apiKey: apiKey, namespace: namespace,
		maxRetries: retries, http: cfg.HTTPClient, cfg: cfg, raw: generated.NewAPIClient(cfg),
	}
	client.Memory = Memory{client: client}
	client.Tasks = Tasks{client: client}
	client.Events = Events{client: client}
	return client, nil
}

func (c *Client) String() string {
	token := "unset"
	if c.token != "" {
		token = "set"
	}
	key := "unset"
	if c.apiKey != "" {
		key = "set"
	}
	return "Client(baseURL=" + c.baseURL + ", token=" + token + ", apiKey=" + key + ")"
}

func (c *Client) Health(ctx context.Context) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := c.invoke(ctx, "health", "", func(ctx context.Context) (*http.Response, error) {
		value, resp, err := c.raw.HealthAPI.Health(ctx).Execute()
		out = value
		return resp, err
	})
	return out, err
}

func (c *Client) auth(ctx context.Context) context.Context {
	if c.token != "" {
		ctx = context.WithValue(ctx, generated.ContextAccessToken, c.token)
	}
	if c.apiKey != "" {
		ctx = context.WithValue(ctx, generated.ContextAPIKeys, map[string]generated.APIKey{
			"ApiKeyAuth": {Key: c.apiKey},
		})
	}
	return ctx
}

func (c *Client) invoke(ctx context.Context, name, idempotencyKey string, op func(context.Context) (*http.Response, error)) error {
	idempotentCall := isIdempotent(name, idempotencyKey)
	attempt := 0
	for {
		callCtx := c.auth(ctx)
		if idempotencyKey != "" {
			c.cfg.AddDefaultHeader("Idempotency-Key", idempotencyKey)
		}
		resp, err := op(callCtx)
		if idempotencyKey != "" {
			delete(c.cfg.DefaultHeader, "Idempotency-Key")
		}
		if err == nil && (resp == nil || resp.StatusCode < 400) {
			return nil
		}
		mapped := mapErr(resp, err)
		if attempt >= c.maxRetries || !retryable(mapped, idempotentCall) {
			return mapped
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff(attempt, mapped)):
		}
		attempt++
	}
}

func mapErr(resp *http.Response, err error) error {
	if err == nil && resp != nil && resp.StatusCode < 400 {
		return nil
	}
	status := 0
	var headers http.Header
	if resp != nil {
		status = resp.StatusCode
		headers = resp.Header
	}
	body := ""
	var gen *generated.GenericOpenAPIError
	if errors.As(err, &gen) && gen != nil {
		body = string(gen.Body())
	} else if err != nil && status == 0 {
		return &ConnectionError{Base: &Error{Message: err.Error()}}
	}
	code, message, details := parseBody(body)
	if message == "" {
		if err != nil {
			message = err.Error()
		} else {
			message = "request failed"
		}
	}
	base := &Error{Status: status, Code: code, Message: message, Details: details}
	if headers != nil {
		base.RequestID = headers.Get("X-Request-ID")
		if raw := headers.Get("Retry-After"); raw != "" {
			if seconds, scanErr := strconv.ParseFloat(raw, 64); scanErr == nil {
				base.RetryAfter = &seconds
			}
		}
	}
	switch {
	case status == 401 || status == 403:
		return &AuthError{Base: base}
	case status == 404:
		return &NotFoundError{Base: base}
	case status == 400 || status == 422:
		return &ValidationError{Base: base}
	case status == 429:
		return &RateLimitError{Base: base}
	case status >= 500:
		return &ServerError{Base: base}
	case status == 0:
		return &ConnectionError{Base: base}
	default:
		return base
	}
}

func parseBody(body string) (string, string, any) {
	if body == "" {
		return "", "", nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", body, nil
	}
	code, _ := payload["code"].(string)
	message, _ := payload["message"].(string)
	if message == "" {
		message, _ = payload["detail"].(string)
	}
	return code, message, payload["details"]
}

func env(name string) string { return strings.TrimSpace(os.Getenv(name)) }

// Memory is the memory sub-client.
type Memory struct{ client *Client }

func (m Memory) Remember(ctx context.Context, content string, vector []float32, tags []string, importance *float64, idempotencyKey string) (*generated.MemoryOperationResponse, error) {
	body := generated.NewRememberRequestModel(content)
	if vector != nil {
		body.SetVector(vector)
	}
	if tags != nil {
		body.SetTags(tags)
	}
	if importance != nil {
		body.SetImportance(float32(*importance))
	}
	var out *generated.MemoryOperationResponse
	err := m.client.invoke(ctx, "remember", idempotencyKey, func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.Remember(ctx).RememberRequestModel(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) Recall(ctx context.Context, query string, limit *int32, vector []float32) (*generated.RecallResponseModel, error) {
	body := generated.NewRecallRequestModel(query)
	reinforce := false
	body.SetReinforce(reinforce)
	if limit != nil {
		body.SetLimit(*limit)
	}
	if vector != nil {
		body.SetVector(vector)
	}
	var out *generated.RecallResponseModel
	err := m.client.invoke(ctx, "recall", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.Recall(ctx).RecallRequestModel(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) Get(ctx context.Context, memoryID string) (*generated.MemoryRow, error) {
	var out *generated.MemoryRow
	err := m.client.invoke(ctx, "get", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.GetMemory(ctx, memoryID)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

// List yields every row, following next_cursor until has_more is false.
func (m Memory) List(ctx context.Context, limit int32) iter.Seq2[generated.MemoryRow, error] {
	return func(yield func(generated.MemoryRow, error) bool) {
		cursor := ""
		seen := map[string]struct{}{}
		for {
			var page *generated.MemoryTableResponse
			err := m.client.invoke(ctx, "list", "", func(ctx context.Context) (*http.Response, error) {
				req := m.client.raw.MemoryAPI.ListMemories(ctx)
				if cursor != "" {
					req = req.Cursor(cursor)
				}
				if limit > 0 {
					req = req.Limit(limit)
				}
				if m.client.namespace != "" {
					req = req.XEbbNamespace(m.client.namespace)
				}
				value, resp, callErr := req.Execute()
				page = value
				return resp, callErr
			})
			if err != nil {
				yield(generated.MemoryRow{}, err)
				return
			}
			if page != nil {
				for _, row := range page.GetItems() {
					if !yield(row, nil) {
						return
					}
				}
			}
			if page == nil || page.HasMore == nil || !*page.HasMore || !page.NextCursor.IsSet() || page.NextCursor.Get() == nil {
				return
			}
			next := *page.NextCursor.Get()
			if next == "" {
				return
			}
			if _, ok := seen[next]; ok {
				return
			}
			seen[next] = struct{}{}
			cursor = next
		}
	}
}

func (m Memory) Status(ctx context.Context) (*generated.MemoryStatusResponse, error) {
	var out *generated.MemoryStatusResponse
	err := m.client.invoke(ctx, "status", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.Status(ctx)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) Forget(ctx context.Context, memoryID string, purge bool) (*generated.ResponseForgetmemory, error) {
	var out *generated.ResponseForgetmemory
	err := m.client.invoke(ctx, "forget", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.ForgetMemory(ctx, memoryID).Purge(purge)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) RememberBatch(ctx context.Context, items []map[string]any, idempotencyKey string) (*generated.BatchRememberResponse, error) {
	encoded := make([]generated.ItemsInner, 0, len(items))
	for _, item := range items {
		cloned := item
		encoded = append(encoded, generated.ItemsInner{MapmapOfStringAny: &cloned})
	}
	body := generated.NewBatchRememberRequestModel(encoded)
	var out *generated.BatchRememberResponse
	err := m.client.invoke(ctx, "rememberBatch", idempotencyKey, func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.RememberBatch(ctx).BatchRememberRequestModel(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) RecallFederated(ctx context.Context, query string, limit *int32) ([]generated.FederatedRecallCandidate, error) {
	body := generated.NewFederatedRecallRequest(query)
	if limit != nil {
		body.SetLimit(*limit)
	}
	var out []generated.FederatedRecallCandidate
	err := m.client.invoke(ctx, "recallFederated", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.RecallFederated(ctx).FederatedRecallRequest(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) Update(ctx context.Context, memoryID string, request generated.UpdateMemoryRequest) (*generated.MemoryRow, error) {
	var out *generated.MemoryRow
	err := m.client.invoke(ctx, "update", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.UpdateMemory(ctx, memoryID).UpdateMemoryRequest(request)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) Reinforce(ctx context.Context, memoryID string, delta float64) (*generated.ReinforceResponse, error) {
	id := memoryID
	body := generated.NewReinforceRequestModel(generated.MemoryId1{String: &id})
	body.SetDelta(float32(delta))
	var out *generated.ReinforceResponse
	err := m.client.invoke(ctx, "reinforce", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.Reinforce(ctx).ReinforceRequestModel(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) Reflect(ctx context.Context, namespace string) (*generated.ReflectResponse, error) {
	body := generated.NewReflectRequestModel()
	ns := namespace
	if ns == "" {
		ns = m.client.namespace
	}
	if ns != "" {
		body.SetNamespace(ns)
	}
	var out *generated.ReflectResponse
	err := m.client.invoke(ctx, "reflect", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.Reflect(ctx).ReflectRequestModel(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (m Memory) WhyNot(ctx context.Context, memoryID, query string) (*generated.WhyNotResponse, error) {
	id := memoryID
	body := generated.NewWhyNotRequestModel(generated.MemoryId1{String: &id}, query)
	var out *generated.WhyNotResponse
	err := m.client.invoke(ctx, "whyNot", "", func(ctx context.Context) (*http.Response, error) {
		req := m.client.raw.MemoryAPI.WhyNot(ctx).WhyNotRequestModel(*body)
		if m.client.namespace != "" {
			req = req.XEbbNamespace(m.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

// Tasks is the task sub-client.
type Tasks struct{ client *Client }

func (t Tasks) Get(ctx context.Context, taskID string) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := t.client.invoke(ctx, "tasks.get", "", func(ctx context.Context) (*http.Response, error) {
		value, resp, callErr := t.client.raw.TasksAPI.GetTask(ctx, taskID).Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (t Tasks) List(ctx context.Context, limit *int32) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := t.client.invoke(ctx, "tasks.list", "", func(ctx context.Context) (*http.Response, error) {
		req := t.client.raw.TasksAPI.ListTasks(ctx)
		if limit != nil {
			req = req.Limit(*limit)
		}
		if t.client.namespace != "" {
			req = req.Namespace(t.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

func (t Tasks) Cancel(ctx context.Context, taskID string) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := t.client.invoke(ctx, "tasks.cancel", "", func(ctx context.Context) (*http.Response, error) {
		value, resp, callErr := t.client.raw.TasksAPI.CancelTask(ctx, taskID).Execute()
		out = value
		return resp, callErr
	})
	return out, err
}

// Events opens the generated streamEvents call.
type Events struct{ client *Client }

func (e Events) Stream(ctx context.Context, tokenQuery string) (*generated.StreamEventsResponse200, error) {
	var out *generated.StreamEventsResponse200
	err := e.client.invoke(ctx, "events.stream", "", func(ctx context.Context) (*http.Response, error) {
		req := e.client.raw.EventsAPI.StreamEvents(ctx)
		if tokenQuery != "" {
			req = req.Token(tokenQuery)
		}
		if e.client.namespace != "" {
			req = req.Namespace(e.client.namespace)
		}
		value, resp, callErr := req.Execute()
		out = value
		return resp, callErr
	})
	return out, err
}
