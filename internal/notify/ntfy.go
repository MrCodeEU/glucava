package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Ntfy posts to an ntfy topic URL such as https://ntfy.sh/my-topic.
type Ntfy struct {
	URL    string
	Token  string // optional bearer token for protected topics
	Client *http.Client
}

// Name implements Channel.
func (n *Ntfy) Name() string { return "ntfy" }

// Send implements Channel.
func (n *Ntfy) Send(ctx context.Context, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", m.Title)
	req.Header.Set("Priority", ntfyPriority(m.Severity))
	req.Header.Set("Tags", ntfyTag(m.Severity)+",glucava")
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
	}
	return do(n.Client, req, "ntfy")
}

func ntfyPriority(sev string) string {
	switch sev {
	case "error":
		return "4"
	case "warning":
		return "3"
	}
	return "2"
}

func ntfyTag(sev string) string {
	switch sev {
	case "error":
		return "rotating_light"
	case "warning":
		return "warning"
	}
	return "information_source"
}

// do sends req and turns a non-2xx status into an error.
func do(c *http.Client, req *http.Request, name string) error {
	if c == nil {
		// Never follow redirects: they could carry the bearer token or webhook
		// body to a host the user did not configure.
		c = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := c.Do(req)
	if err != nil {
		// url.Error carries the full URL, which for ntfy topics and webhooks is a secret.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: HTTP %d", name, resp.StatusCode)
	}
	return nil
}
