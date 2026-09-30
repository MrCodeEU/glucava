package notify

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/MrCodeEU/glucava/internal/secrets"
)

// PushSub is one browser or phone registered for Web Push. Endpoint, P256dh
// and Auth are exactly what the browser's PushSubscription holds.
type PushSub struct {
	ID        string
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent string
	Created   time.Time
	LastOK    time.Time // last accepted delivery; zero if none yet
	LastError string    // last failure, empty after a success
}

// PushStore is where subscriptions live.
type PushStore interface {
	PushSubscriptions(ctx context.Context) ([]PushSub, error)
	// PushResult records the outcome of a delivery: errText is empty on success.
	PushResult(ctx context.Context, endpoint string, at time.Time, errText string) error
	DeletePushSubscription(ctx context.Context, endpoint string) error
}

// SecretStore is the part of the secrets vault the push channel needs.
type SecretStore interface {
	Get(name string) (value string, ok bool, err error)
	Set(name, value string) error
}

// fallbackSubject is the VAPID contact used when the operator has set neither
// a public URL nor an email address. Push services want a contact they could
// reach; the project page is a valid https one.
const fallbackSubject = "https://github.com/MrCodeEU/glucava"

// maxPayloadBody caps the notification text. Web Push allows about 4 KB in
// total; glucava's messages are short, and a long error should not be lost
// to the encrypted record limit.
const maxPayloadBody = 400

// WebPush delivers a message to every subscribed browser.
type WebPush struct {
	Store   PushStore
	Private string // VAPID private key, base64url
	Public  string // VAPID public key, base64url
	Subject string // VAPID contact: an https URL or an email address
	// Wants reports whether this kind of message should be pushed. Nil sends all.
	Wants  func(msgType string) bool
	Client webpush.HTTPClient // nil uses a client that refuses private addresses
	Now    func() time.Time

	clientOnce sync.Once
	client     webpush.HTTPClient
}

// Name implements Channel.
func (w *WebPush) Name() string { return "webpush" }

func (w *WebPush) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WebPush) httpClient() webpush.HTTPClient {
	if w.Client != nil {
		return w.Client
	}
	w.clientOnce.Do(func() { w.client = safePushClient() })
	return w.client
}

// PushPayload is what the service worker receives. It stays small and holds
// no glucose numbers beyond what the message text already says.
type PushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`           // a path on this site, for the click
	Tag   string `json:"tag,omitempty"` // same tag replaces an earlier notification
}

// Send implements Channel. It succeeds when at least one device accepted the
// message; a subscription the push service reports as gone (404 or 410) is
// removed.
func (w *WebPush) Send(ctx context.Context, m Message) error {
	if w.Wants != nil && !w.Wants(m.Type) {
		return nil
	}
	if w.Store == nil || w.Private == "" || w.Public == "" {
		return errors.New("notify: web push: not configured")
	}
	subs, err := w.Store.PushSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("notify: web push: %w", err)
	}
	if len(subs) == 0 {
		return errors.New("notify: web push: no device is subscribed")
	}
	payload, err := json.Marshal(pushPayload(m))
	if err != nil {
		return err
	}
	ttl, urgency := pushTiming(m)
	opts := &webpush.Options{
		HTTPClient: w.httpClient(), Subscriber: w.subject(),
		VAPIDPublicKey: w.Public, VAPIDPrivateKey: w.Private,
		TTL: ttl, Urgency: urgency,
	}

	var errs []error
	ok := 0
	for _, sub := range subs {
		status, err := w.sendOne(ctx, sub, payload, opts)
		now := w.now()
		switch {
		case err == nil && status/100 == 2:
			ok++
			if rerr := w.Store.PushResult(ctx, sub.Endpoint, now, ""); rerr != nil {
				slog.Error("push: record result", "err", rerr)
			}
		case err == nil && (status == http.StatusNotFound || status == http.StatusGone):
			slog.Info("push: subscription expired, removing it", "host", endpointHost(sub.Endpoint), "status", status)
			if derr := w.Store.DeletePushSubscription(ctx, sub.Endpoint); derr != nil {
				slog.Error("push: remove subscription", "err", derr)
			}
		default:
			if err == nil {
				err = fmt.Errorf("HTTP %d", status)
			}
			err = fmt.Errorf("%s: %w", endpointHost(sub.Endpoint), err)
			errs = append(errs, err)
			if rerr := w.Store.PushResult(ctx, sub.Endpoint, now, err.Error()); rerr != nil {
				slog.Error("push: record result", "err", rerr)
			}
		}
	}
	if ok > 0 {
		return nil
	}
	if len(errs) == 0 {
		// Every subscription was gone: nothing was delivered, and nobody is left to try.
		return errors.New("notify: web push: every subscribed device had expired and was removed")
	}
	return fmt.Errorf("notify: web push: %w", errors.Join(errs...))
}

func (w *WebPush) sendOne(ctx context.Context, sub PushSub, payload []byte, opts *webpush.Options) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := webpush.SendNotificationWithContext(ctx, payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}}, opts)
	if err != nil {
		// url.Error carries the endpoint, which is a secret capability URL.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, nil
}

func (w *WebPush) subject() string {
	if s := strings.TrimSpace(w.Subject); s != "" {
		return s
	}
	return fallbackSubject
}

// Subject picks the VAPID contact from the settings: the public URL when it is
// https, else the notification email address, else the project page.
func Subject(publicURL, emailTo string) string {
	if u := strings.TrimSpace(publicURL); strings.HasPrefix(u, "https://") {
		return u
	}
	if e := strings.TrimSpace(emailTo); e != "" && strings.Contains(e, "@") {
		return e
	}
	return fallbackSubject
}

// pushPayload builds the small JSON the service worker shows.
func pushPayload(m Message) PushPayload {
	title := strings.TrimSpace(m.Title)
	if title == "" {
		title = "glucava"
	}
	tag := m.Type
	if m.StravaID != "" {
		tag += ":" + m.StravaID
	}
	return PushPayload{Title: clip(title, 100), Body: clip(oneLine(m.Body), maxPayloadBody), URL: pushPath(m), Tag: tag}
}

// pushPath is the page inside the web UI that a tap on the notification
// opens. It reuses LinkFor, so the two never disagree, and keeps only the
// path: the service worker opens it on whatever origin it runs on.
func pushPath(m Message) string {
	href, _ := LinkFor("https://x.invalid", m)
	u, err := url.Parse(href)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}

// pushTiming picks how long the push service may hold a message and how
// urgently it is delivered. Alerts want attention and stay relevant for a few
// hours; a summary is stale sooner; a test only matters right now.
func pushTiming(m Message) (ttl int, urgency webpush.Urgency) {
	switch {
	case m.Type == TypeTest:
		return 60, webpush.UrgencyHigh
	case m.Type == TypeActivitySummary:
		return 6 * 3600, webpush.UrgencyNormal
	case m.Type == TypeWeeklySummary || m.Type == TypeHealthReport:
		return 24 * 3600, webpush.UrgencyLow
	case m.Severity == "error":
		return 4 * 3600, webpush.UrgencyHigh
	}
	return 4 * 3600, webpush.UrgencyNormal
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip shortens s to at most n runes, marking the cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func endpointHost(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return "push service"
}

// ValidPushEndpoint reports whether raw is a subscription endpoint glucava
// is willing to call: an https URL on a public host. The server connects to
// it, so an address pointing at the local network is refused.
func ValidPushEndpoint(raw string) error {
	if len(raw) == 0 || len(raw) > 2048 {
		return errors.New("the push endpoint is missing or too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("the push endpoint must be an https URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("the push endpoint must be a public address")
	}
	if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
		return errors.New("the push endpoint must be a public address")
	}
	return nil
}

// ValidPushKeys checks the browser's keys: p256dh is an uncompressed P-256
// point (65 bytes) and auth a 16-byte secret, both base64url.
func ValidPushKeys(p256dh, auth string) error {
	dh, err := decodeB64(p256dh)
	if err != nil || len(dh) != 65 || dh[0] != 4 {
		return errors.New("the p256dh key is not valid")
	}
	if _, err := ecdh.P256().NewPublicKey(dh); err != nil {
		return errors.New("the p256dh key is not a point on the curve")
	}
	a, err := decodeB64(auth)
	if err != nil || len(a) != 16 {
		return errors.New("the auth secret is not valid")
	}
	return nil
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

// blockedIP is an address the server must never push to: loopback, private,
// link-local, unspecified, multicast and carrier-grade NAT ranges.
func blockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		if v4[0] == 100 && v4[1]&0xc0 == 64 { // 100.64.0.0/10
			return true
		}
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

// safePushClient is an HTTP client that connects only to public addresses,
// whatever a hostname resolves to, and never follows redirects.
func safePushClient() *http.Client {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || blockedIP(ip) {
				return errors.New("refusing to connect to a non-public address")
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext:         d.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        4,
			IdleConnTimeout:     30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// DemoPushClient pretends every push service accepted the message and only
// logs it, so demo mode shows the feature without contacting anyone.
func DemoPushClient() webpush.HTTPClient { return demoPushClient{} }

type demoPushClient struct{}

func (demoPushClient) Do(req *http.Request) (*http.Response, error) {
	slog.Info("demo web push (not sent)", "host", req.URL.Host)
	return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody, Header: http.Header{}, Request: req}, nil
}

var vapidMu sync.Mutex

// EnsureVAPID returns the VAPID key pair, generating and storing the private
// key on first use. The public key is derived from it.
func EnsureVAPID(v SecretStore) (private, public string, err error) {
	vapidMu.Lock()
	defer vapidMu.Unlock()
	private, ok, err := v.Get(secrets.NameVAPIDPrivate)
	if err != nil {
		return "", "", err
	}
	if ok {
		if public, err = VAPIDPublic(private); err == nil {
			return private, public, nil
		}
		// A stored value that does not decode is replaced rather than
		// leaving push broken for good; devices re-subscribe on their next visit.
		slog.Warn("push: stored VAPID key is unusable, generating a new one", "err", err)
	}
	private, public, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	if err := v.Set(secrets.NameVAPIDPrivate, private); err != nil {
		return "", "", err
	}
	return private, public, nil
}

// VAPIDPublic derives the public key (base64url, uncompressed point) from a
// base64url private key.
func VAPIDPublic(private string) (string, error) {
	raw, err := decodeB64(private)
	if err != nil {
		return "", err
	}
	k, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// NewWebPush builds the channel from the stored VAPID key and the settings.
// demo swaps the network for a logging stand-in.
func NewWebPush(st PushStore, v SecretStore, publicURL, emailTo string, demo bool) (*WebPush, error) {
	priv, pub, err := EnsureVAPID(v)
	if err != nil {
		return nil, err
	}
	w := &WebPush{Store: st, Private: priv, Public: pub, Subject: Subject(publicURL, emailTo)}
	if demo {
		w.Client = DemoPushClient()
	}
	return w, nil
}
