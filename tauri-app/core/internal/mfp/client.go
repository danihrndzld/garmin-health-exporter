package mfp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	ErrSession     = errors.New("the MyFitnessPal session is missing or expired")
	ErrRateLimited = errors.New("MyFitnessPal is rate limiting requests (HTTP 429)")
)

const (
	wwwBase    = "https://www.myfitnesspal.com"
	apiBase    = "https://api.myfitnesspal.com"
	diaryTypes = "food_entry,exercise_entry,steps_aggregate" // an unknown type makes MFP return nothing
)

var loopback = regexp.MustCompile(`^http://(127\.0\.0\.1|localhost):\d+$`)

// FakeServer returns MFP_FAKE_SERVER when it is a loopback URL, else "".
// It is read on every request, so it can never redirect traffic elsewhere.
func FakeServer() string {
	v := os.Getenv("MFP_FAKE_SERVER")
	if loopback.MatchString(v) {
		return v
	}
	return ""
}

type Options struct {
	Gap  time.Duration // pause between requests; 1 s against the real MFP
	Base string        // test server; overrides both hosts
	HTTP *http.Client
}

type Client struct {
	sess     *Session
	opts     Options
	token    string
	userID   string
	changed  bool
	lastCall time.Time
}

func NewClient(s *Session, opts Options) *Client {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Gap == 0 && opts.Base == "" && FakeServer() == "" {
		opts.Gap = time.Second // the real MFP: at least 1 s between requests
	}
	return &Client{sess: s, opts: opts}
}

func (c *Client) Session() *Session    { return c.sess }
func (c *Client) SessionChanged() bool { return c.changed }
func (c *Client) userAgent() string    { return c.sess.UserAgent }

func (c *Client) host(real string) string {
	if c.opts.Base != "" {
		return c.opts.Base
	}
	if f := FakeServer(); f != "" {
		return f
	}
	return real
}

func (c *Client) cookieHeader() string {
	parts := make([]string, 0, len(c.sess.Cookies))
	for _, ck := range c.sess.Cookies {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}

func (c *Client) do(ctx context.Context, u string, api bool) (*http.Response, []byte, error) {
	if wait := c.opts.Gap - time.Since(c.lastCall); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	// The pause counts from the end of the previous request, once its body is read.
	defer func() { c.lastCall = time.Now() }()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Cookie", c.cookieHeader())
	if api {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("mfp-client-id", "mfp-main-js")
		req.Header.Set("mfp-user-id", c.userID)
		req.Header.Set("Accept", "application/json")
	}
	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if c.sess.Merge(resp.Cookies()) {
		c.changed = true
	}
	return resp, body, err
}

// Auth trades the browser session for a bearer token. An HTML answer means the
// session is no longer valid.
func (c *Client) Auth(ctx context.Context) error {
	resp, body, err := c.do(ctx, c.host(wwwBase)+"/user/auth_token?refresh=true", false)
	if err != nil {
		return err
	}
	if resp.StatusCode == 429 {
		return ErrRateLimited
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return ErrSession
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		UserID      any    `json:"user_id"`
	}
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return ErrSession
	}
	c.token, c.userID = tok.AccessToken, fmt.Sprint(tok.UserID)
	return nil
}

// apiGet calls the JSON API; a 401 refreshes the token once and tries again.
func (c *Client) apiGet(ctx context.Context, u string) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		resp, body, err := c.do(ctx, u, true)
		if err != nil {
			return nil, err
		}
		switch {
		case resp.StatusCode == 429:
			return nil, ErrRateLimited
		case resp.StatusCode == 401 && attempt == 0:
			if err := c.Auth(ctx); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode == 401:
			return nil, ErrSession
		case resp.StatusCode != 200:
			return nil, fmt.Errorf("MyFitnessPal answered HTTP %d", resp.StatusCode)
		}
		return body, nil
	}
	return nil, ErrSession
}

func (c *Client) Diary(ctx context.Context, date string) ([]DiaryItem, error) {
	q := url.Values{"entry_date": {date}, "types": {diaryTypes}}
	body, err := c.apiGet(ctx, c.host(apiBase)+"/v2/diary?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []DiaryItem `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("diary for %s is not valid JSON", date)
	}
	return out.Items, nil
}

func (c *Client) Goals(ctx context.Context, date string) (Goals, error) {
	body, err := c.apiGet(ctx, c.host(apiBase)+"/v2/nutrient-goals?date="+url.QueryEscape(date))
	if err != nil {
		return Goals{}, err
	}
	var out struct {
		Items []struct {
			Default struct {
				Energy       *Energy  `json:"energy"`
				Protein      *float64 `json:"protein"`
				Carbohydrate *float64 `json:"carbohydrates"`
				Fat          *float64 `json:"fat"`
				Sodium       *float64 `json:"sodium"`
				Sugar        *float64 `json:"sugar"`
			} `json:"default_goal"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Goals{}, fmt.Errorf("goals for %s are not valid JSON", date)
	}
	if len(out.Items) == 0 {
		return Goals{}, nil
	}
	d := out.Items[0].Default
	return Goals{EnergyKcal: d.Energy.Kcal(), Protein: d.Protein, Carbohydrate: d.Carbohydrate, Fat: d.Fat, Sodium: d.Sodium, Sugar: d.Sugar}, nil
}
