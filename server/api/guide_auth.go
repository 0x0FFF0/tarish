package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"tarish-server/models"
)

var ErrInvalidGuideEditChallenge = errors.New("invalid or expired guide edit challenge")
var ErrInvalidGuideEditToken = errors.New("invalid or expired guide edit token")

type guideEditVerifier struct {
	mu         sync.Mutex
	challenges map[string]guideChallenge
	sessions   map[string]time.Time
}

type guideChallenge struct {
	answer    string
	expiresAt time.Time
}

func newGuideEditVerifier() *guideEditVerifier {
	return &guideEditVerifier{
		challenges: make(map[string]guideChallenge),
		sessions:   make(map[string]time.Time),
	}
}

func (v *guideEditVerifier) StartChallenge() (*models.GuideEditChallenge, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.cleanupLocked(time.Now().UTC())

	challengeID, err := randomHex(12)
	if err != nil {
		return nil, err
	}

	answer, err := randomCode()
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	v.challenges[challengeID] = guideChallenge{
		answer:    answer,
		expiresAt: expiresAt,
	}

	return &models.GuideEditChallenge{
		ChallengeID: challengeID,
		Prompt:      "Type " + answer + " to unlock document editing.",
		ExpiresAt:   expiresAt,
	}, nil
}

func (v *guideEditVerifier) VerifyChallenge(challengeID, answer string) (*models.GuideEditSession, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now().UTC()
	v.cleanupLocked(now)

	challenge, ok := v.challenges[strings.TrimSpace(challengeID)]
	if !ok || now.After(challenge.expiresAt) {
		return nil, ErrInvalidGuideEditChallenge
	}

	if strings.TrimSpace(strings.ToUpper(answer)) != challenge.answer {
		return nil, ErrInvalidGuideEditChallenge
	}

	delete(v.challenges, challengeID)

	token, err := randomHex(24)
	if err != nil {
		return nil, err
	}

	expiresAt := now.Add(15 * time.Minute)
	v.sessions[token] = expiresAt

	return &models.GuideEditSession{
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func (v *guideEditVerifier) ValidateToken(token string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now().UTC()
	v.cleanupLocked(now)

	expiresAt, ok := v.sessions[strings.TrimSpace(token)]
	if !ok || now.After(expiresAt) {
		return false
	}

	return true
}

func (v *guideEditVerifier) cleanupLocked(now time.Time) {
	for id, challenge := range v.challenges {
		if now.After(challenge.expiresAt) {
			delete(v.challenges, id)
		}
	}
	for token, expiresAt := range v.sessions {
		if now.After(expiresAt) {
			delete(v.sessions, token)
		}
	}
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomCode() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}
