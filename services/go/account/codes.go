package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Verification-code channels and purposes. Purposes segregate codes so one
// issued for registration cannot be replayed to, say, reset a password.
const (
	channelEmail = "email"
	channelSMS   = "sms"

	purposeRegister = "register"
	purposeLogin    = "login"
	purposeReset    = "reset_password"
	purposeAddress  = "address"
	purposeBind     = "bind"
)

// EmailSender delivers a verification code to an email address. The local
// implementation only logs; a real provider is wired in later.
type EmailSender interface {
	SendEmailCode(ctx context.Context, email, code, purpose string) error
}

// SmsSender delivers a verification code to a phone number (E.164). The local
// implementation only logs; a real provider is wired in later.
type SmsSender interface {
	SendSMSCode(ctx context.Context, phone, code, purpose string) error
}

// CodeRecorder captures the most recent plaintext code per (target, purpose).
// It backs the dev-only "last code" endpoint so tests and smoke scripts can read
// a code without a real email/SMS provider. It is never populated in production
// (the server only constructs it when DevMode is on).
type CodeRecorder struct {
	mu   sync.Mutex
	last map[string]string
}

// NewCodeRecorder returns an empty recorder.
func NewCodeRecorder() *CodeRecorder { return &CodeRecorder{last: map[string]string{}} }

func recorderKey(target, purpose string) string { return purpose + "|" + target }

func (r *CodeRecorder) record(target, purpose, code string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last[recorderKey(target, purpose)] = code
}

// Last returns the most recent code for target+purpose, or "" if none.
func (r *CodeRecorder) Last(target, purpose string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last[recorderKey(target, purpose)]
}

// logEmailSender is the local EmailSender: it logs the code and records it.
type logEmailSender struct{ rec *CodeRecorder }

func (s logEmailSender) SendEmailCode(_ context.Context, email, code, purpose string) error {
	log.Printf("[mock-email] to=%s purpose=%s code=%s", email, purpose, code)
	s.rec.record(email, purpose, code)
	return nil
}

// logSmsSender is the local SmsSender: it logs the code and records it.
type logSmsSender struct{ rec *CodeRecorder }

func (s logSmsSender) SendSMSCode(_ context.Context, phone, code, purpose string) error {
	log.Printf("[mock-sms] to=%s purpose=%s code=%s", phone, purpose, code)
	s.rec.record(phone, purpose, code)
	return nil
}

// ErrRateLimited is returned when a target requests codes too frequently.
var ErrRateLimited = errors.New("verification code requested too frequently")

// CodeService issues and verifies hashed verification codes with per-target
// rate limiting. Rate limiting uses Redis when available and falls back to the
// database counters otherwise.
type CodeService struct {
	store       *Store
	email       EmailSender
	sms         SmsSender
	rec         *CodeRecorder
	redis       *redis.Client
	ttl         time.Duration
	maxAttempts int
	minInterval time.Duration // minimum gap between two sends to one target+purpose
	hourlyMax   int           // max sends per target+purpose per hour
	now         func() time.Time
}

// NewCodeService constructs a CodeService with sensible defaults.
func NewCodeService(store *Store, email EmailSender, sms SmsSender, rec *CodeRecorder, rdb *redis.Client, now func() time.Time) *CodeService {
	return &CodeService{
		store:       store,
		email:       email,
		sms:         sms,
		rec:         rec,
		redis:       rdb,
		ttl:         10 * time.Minute,
		maxAttempts: 5,
		minInterval: 60 * time.Second,
		hourlyMax:   5,
		now:         now,
	}
}

// Send generates, stores and dispatches a code for target over channel. It is
// rate-limited; a violation returns ErrRateLimited.
func (c *CodeService) Send(ctx context.Context, channel, target, purpose string) error {
	ok, err := c.allowSend(ctx, target, purpose)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRateLimited
	}
	code, err := generateNumericCode(6)
	if err != nil {
		return err
	}
	expires := c.now().Add(c.ttl)
	if err := c.store.InsertCode(ctx, target, channel, purpose, hashToken(code), c.maxAttempts, expires); err != nil {
		return err
	}
	switch channel {
	case channelEmail:
		return c.email.SendEmailCode(ctx, target, code, purpose)
	case channelSMS:
		return c.sms.SendSMSCode(ctx, target, code, purpose)
	default:
		return fmt.Errorf("unknown channel %q", channel)
	}
}

// Verify checks a submitted code against the stored hash.
func (c *CodeService) Verify(ctx context.Context, target, purpose, code string) (codeVerdict, error) {
	return c.store.VerifyAndConsumeCode(ctx, target, purpose, hashToken(code))
}

// allowSend enforces the min-interval and hourly-cap limits.
func (c *CodeService) allowSend(ctx context.Context, target, purpose string) (bool, error) {
	if c.redis != nil {
		return c.allowSendRedis(ctx, target, purpose)
	}
	// Database fallback: count codes issued in each window.
	now := c.now()
	recent, err := c.store.CountCodesSince(ctx, target, purpose, now.Add(-c.minInterval))
	if err != nil {
		return false, err
	}
	if recent >= 1 {
		return false, nil
	}
	hourly, err := c.store.CountCodesSince(ctx, target, purpose, now.Add(-time.Hour))
	if err != nil {
		return false, err
	}
	return hourly < c.hourlyMax, nil
}

// allowSendRedis implements the rate limits with Redis counters (fixed window).
func (c *CodeService) allowSendRedis(ctx context.Context, target, purpose string) (bool, error) {
	intervalKey := "vc:int:" + purpose + ":" + target
	if c.minInterval > 0 {
		// SetNX with a zero TTL would never expire; only guard when a real
		// interval is configured (dev mode sets minInterval to 0).
		set, err := c.redis.SetNX(ctx, intervalKey, 1, c.minInterval).Result()
		if err != nil {
			return false, err
		}
		if !set {
			return false, nil // a send happened within minInterval
		}
	}
	hourKey := "vc:hr:" + purpose + ":" + target
	n, err := c.redis.Incr(ctx, hourKey).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		_ = c.redis.Expire(ctx, hourKey, time.Hour).Err()
	}
	if n > int64(c.hourlyMax) {
		// Roll back the interval guard so the cap is the binding limit.
		_ = c.redis.Del(ctx, intervalKey).Err()
		return false, nil
	}
	return true, nil
}

// generateNumericCode returns a cryptographically random n-digit decimal code.
func generateNumericCode(n int) (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", n, v), nil
}
