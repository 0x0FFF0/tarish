package alerter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"tarish-server/models"
	"tarish-server/store"
)

// Alerter is the long-lived ticker that watches miners and pushes Bark
// notifications when one drops offline.
type Alerter struct {
	store *store.Store
	bark  BarkSender

	reloadCh chan struct{}

	mu       sync.RWMutex
	settings *models.BarkSettings
}

const (
	barkGroup        = "tarish"
	barkSoundOffline = "alarm"
	barkSoundOK      = "" // default sound
	maxConcurrentTx  = 5  // concurrent Bark sends per tick
)

// New constructs an alerter. The caller must call Run(ctx) on a goroutine.
func New(s *store.Store, bark BarkSender) *Alerter {
	return &Alerter{
		store:    s,
		bark:     bark,
		reloadCh: make(chan struct{}, 1),
	}
}

// Reload signals the running loop to re-read settings from the store.
// Non-blocking; if a reload is already pending, the call is a no-op.
func (a *Alerter) Reload() {
	select {
	case a.reloadCh <- struct{}{}:
	default:
	}
}

// CurrentSettings returns the cached settings (read-only snapshot). Used by
// status handlers and for redacted JSON responses.
func (a *Alerter) CurrentSettings() *models.BarkSettings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.settings == nil {
		return nil
	}
	cp := *a.settings
	return &cp
}

// SendTest fires a one-shot test notification using either an explicit token
// override or the stored token. Used by POST /api/settings/bark/test.
func (a *Alerter) SendTest(ctx context.Context, tokenOverride string) error {
	token := strings.TrimSpace(tokenOverride)
	if token == "" {
		current, err := a.store.GetBarkSettings()
		if err != nil {
			return err
		}
		token = strings.TrimSpace(current.Token)
	}
	if token == "" {
		return fmt.Errorf("no Bark token configured")
	}

	title := "Tarish: test notification"
	body := "If you can read this, your Bark setup is wired up correctly."
	err := a.bark.Send(ctx, token, title, body, barkGroup, barkSoundOK)

	entry := &models.AlertLogEntry{
		Kind:  "test",
		Title: title,
		Body:  body,
		OK:    err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	if logErr := a.store.RecordAlertLog(entry); logErr != nil {
		log.Printf("[alerter] failed to record test alert: %v", logErr)
	}
	return err
}

// Run blocks until ctx is cancelled. The ticker period follows
// settings.CheckIntervalS; Reload() rebuilds the ticker on settings change.
func (a *Alerter) Run(ctx context.Context) {
	if err := a.refreshSettings(); err != nil {
		log.Printf("[alerter] failed to load initial settings: %v", err)
	}

	interval := a.tickInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[alerter] started (interval %s)", interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[alerter] stopping: %v", ctx.Err())
			return

		case <-a.reloadCh:
			if err := a.refreshSettings(); err != nil {
				log.Printf("[alerter] reload failed: %v", err)
				continue
			}
			newInterval := a.tickInterval()
			if newInterval != interval {
				ticker.Stop()
				ticker = time.NewTicker(newInterval)
				interval = newInterval
				log.Printf("[alerter] interval changed to %s", interval)
			}

		case <-ticker.C:
			if err := a.tick(ctx); err != nil {
				log.Printf("[alerter] tick error: %v", err)
			}
		}
	}
}

func (a *Alerter) refreshSettings() error {
	settings, err := a.store.GetBarkSettings()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.settings = settings
	a.mu.Unlock()
	return nil
}

func (a *Alerter) tickInterval() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.settings == nil || a.settings.CheckIntervalS <= 0 {
		return 60 * time.Second
	}
	return time.Duration(a.settings.CheckIntervalS) * time.Second
}

func (a *Alerter) tick(ctx context.Context) error {
	settings := a.CurrentSettings()
	if settings == nil || !settings.Enabled || strings.TrimSpace(settings.Token) == "" {
		return nil
	}

	miners, err := a.store.GetMiners()
	if err != nil {
		return fmt.Errorf("get miners: %w", err)
	}
	if len(miners) == 0 {
		return nil
	}

	now := time.Now().UTC()
	muted := settings.MuteUntil != nil && now.Before(*settings.MuteUntil)
	throttle := time.Duration(settings.ThrottleMinutes) * time.Minute

	// Bound concurrent Bark sends so a stuck remote can't stall the tick.
	sem := make(chan struct{}, maxConcurrentTx)
	var wg sync.WaitGroup

	for _, m := range miners {
		miner := m // capture
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a.evaluateMiner(ctx, settings, miner, now, muted, throttle)
		}()
	}
	wg.Wait()
	return nil
}

// evaluateMiner runs the per-miner state machine described in the plan.
func (a *Alerter) evaluateMiner(
	ctx context.Context,
	settings *models.BarkSettings,
	miner *models.Miner,
	now time.Time,
	muted bool,
	throttle time.Duration,
) {
	effective := effectiveStatus(miner.Status)
	prior, err := a.store.GetAlertState(miner.ID)
	if err != nil {
		log.Printf("[alerter] read state for %s: %v", miner.ID, err)
		return
	}

	priorOffline := prior != nil && prior.LastState == "offline"

	switch {
	case !priorOffline && effective == "online":
		// stable online — no work, no row
		return

	case !priorOffline && effective == "offline":
		a.handleNewOffline(ctx, settings, miner, now, muted)

	case priorOffline && effective == "offline":
		a.handleStillOffline(ctx, settings, miner, prior, now, muted, throttle)

	case priorOffline && effective == "online":
		a.handleRecovery(ctx, settings, miner, prior, now, muted)
	}
}

func (a *Alerter) handleNewOffline(
	ctx context.Context,
	settings *models.BarkSettings,
	miner *models.Miner,
	now time.Time,
	muted bool,
) {
	title, body := buildOfflineMessage(miner, now, false, 0)
	state := &models.AlertState{
		MinerID:      miner.ID,
		LastState:    "offline",
		OfflineSince: timePtr(now),
		AlertCount:   1,
	}

	if muted {
		a.recordSuppressed(miner.ID, title, body)
		state.LastAlertedAt = timePtr(now)
		a.persistState(state)
		return
	}

	if err := a.send(ctx, settings.Token, "offline", miner.ID, title, body, barkSoundOffline); err == nil {
		state.LastAlertedAt = timePtr(now)
		a.persistState(state)
		return
	}
	// Send failed. Keep state row so we don't fire infinite "new" events
	// each tick, but leave LastAlertedAt nil so the next tick retries.
	state.AlertCount = 0
	a.persistState(state)
}

func (a *Alerter) handleStillOffline(
	ctx context.Context,
	settings *models.BarkSettings,
	miner *models.Miner,
	prior *models.AlertState,
	now time.Time,
	muted bool,
	throttle time.Duration,
) {
	shouldAttempt := prior.LastAlertedAt == nil ||
		now.Sub(*prior.LastAlertedAt) >= throttle
	if !shouldAttempt {
		return
	}

	since := time.Duration(0)
	if prior.OfflineSince != nil {
		since = now.Sub(*prior.OfflineSince)
	}
	title, body := buildOfflineMessage(miner, now, true, since)

	if muted {
		a.recordSuppressed(miner.ID, title, body)
		next := *prior
		next.LastAlertedAt = timePtr(now)
		next.AlertCount = prior.AlertCount + 1
		a.persistState(&next)
		return
	}

	if err := a.send(ctx, settings.Token, "offline", miner.ID, title, body, barkSoundOffline); err == nil {
		next := *prior
		next.LastAlertedAt = timePtr(now)
		next.AlertCount = prior.AlertCount + 1
		a.persistState(&next)
	}
	// On failure: leave state untouched; next tick retries.
}

func (a *Alerter) handleRecovery(
	ctx context.Context,
	settings *models.BarkSettings,
	miner *models.Miner,
	prior *models.AlertState,
	now time.Time,
	muted bool,
) {
	since := time.Duration(0)
	if prior.OfflineSince != nil {
		since = now.Sub(*prior.OfflineSince)
	}
	title, body := buildRecoveryMessage(miner, since)
	cleared := &models.AlertState{
		MinerID:   miner.ID,
		LastState: "online",
	}

	if muted {
		a.recordSuppressed(miner.ID, title, body)
		a.persistState(cleared)
		return
	}
	if !settings.NotifyRecovery {
		// User opted out of recovery pushes. Still clear the state.
		a.persistState(cleared)
		return
	}

	// Send result does not block clearing state — recovery is a one-shot
	// signal; if Bark is unreachable we don't want to keep the miner stuck
	// in "offline" forever.
	if err := a.send(ctx, settings.Token, "recovery", miner.ID, title, body, barkSoundOK); err != nil {
		log.Printf("[alerter] recovery send failed for %s: %v", miner.ID, err)
	}
	a.persistState(cleared)
}

func (a *Alerter) send(ctx context.Context, token, kind, minerID, title, body, sound string) error {
	err := a.bark.Send(ctx, token, title, body, barkGroup, sound)
	entry := &models.AlertLogEntry{
		MinerID: minerID,
		Kind:    kind,
		Title:   title,
		Body:    body,
		OK:      err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	if logErr := a.store.RecordAlertLog(entry); logErr != nil {
		log.Printf("[alerter] failed to record alert: %v", logErr)
	}
	return err
}

func (a *Alerter) recordSuppressed(minerID, title, body string) {
	entry := &models.AlertLogEntry{
		MinerID: minerID,
		Kind:    "offline_suppressed",
		Title:   title,
		Body:    body,
		OK:      false,
		Error:   "muted",
	}
	if err := a.store.RecordAlertLog(entry); err != nil {
		log.Printf("[alerter] failed to record suppressed alert: %v", err)
	}
}

func (a *Alerter) persistState(state *models.AlertState) {
	if err := a.store.UpsertAlertState(state); err != nil {
		log.Printf("[alerter] upsert state for %s: %v", state.MinerID, err)
	}
}

// effectiveStatus collapses the three-state miner status to a binary signal
// for alerting. "stale" (90s–5min) is treated as online; only true offline
// (>=5min since last_seen) triggers an alert.
func effectiveStatus(s string) string {
	if s == "offline" {
		return "offline"
	}
	return "online"
}

func displayName(m *models.Miner) string {
	if v := strings.TrimSpace(m.WorkerID); v != "" {
		return v
	}
	if v := strings.TrimSpace(m.Hostname); v != "" {
		return v
	}
	return m.ID
}

func buildOfflineMessage(m *models.Miner, now time.Time, isReAlert bool, sinceOffline time.Duration) (string, string) {
	name := displayName(m)
	title := fmt.Sprintf("Tarish: %s offline", name)
	var body strings.Builder
	if isReAlert && sinceOffline > 0 {
		fmt.Fprintf(&body, "Still offline for %s. ", humanDuration(sinceOffline))
	}
	if !m.LastSeen.IsZero() {
		fmt.Fprintf(&body, "Last seen %s ago", humanDuration(now.Sub(m.LastSeen)))
	} else {
		body.WriteString("No recent reports")
	}
	if ip := strings.TrimSpace(m.IP); ip != "" {
		fmt.Fprintf(&body, ", ip %s", ip)
	}
	body.WriteString(".")
	return title, body.String()
}

func buildRecoveryMessage(m *models.Miner, since time.Duration) (string, string) {
	name := displayName(m)
	title := fmt.Sprintf("Tarish: %s recovered", name)
	body := fmt.Sprintf("%s back online", name)
	if since > 0 {
		body += fmt.Sprintf(" (was offline for %s)", humanDuration(since))
	}
	return title, body + "."
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		s := int(d.Seconds())
		if s < 1 {
			s = 1
		}
		return fmt.Sprintf("%ds", s)
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) - hours*60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

func timePtr(t time.Time) *time.Time {
	return &t
}
