package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/audit"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

const (
	defaultDisabledRevivalInterval          = 15 * time.Minute
	defaultDisabledRevivalBatchSize         = 20
	defaultDisabledRevivalConcurrency       = 8
	defaultDisabledRevivalMinDisabledAge    = 30 * time.Minute
	defaultDisabledRevivalFailRetryAfter    = 6 * time.Hour
	defaultDisabledRevivalTimeout           = 2 * time.Minute
	defaultDisabledRevivalTimeoutRetryAfter = 30 * time.Minute
	maxDisabledRevivalBatchSize             = 100
	maxDisabledRevivalConcurrency           = 64
	maxDisabledRevivalManualIDs             = 10000
	disabledRevivalScanLimit                = 5000
	// Natural prompt: do not instruct the model to think. Live traffic also
	// does not, and a forced "think step by step" over-revives accounts that
	// then fail ClassifyQualityHold on real user turns.
	disabledRevivalPrompt          = "A shop sells apples at 3 for $10 or 5 for $16. Which deal is cheaper per apple, and by how many cents? Reply with one integer."
	disabledRevivalMaxOutputTokens = 512
	disabledRevivalModel           = "grok-4.6"
)

// DisabledAccountRevivalRuntime is the background probe that re-enables
// quality-disabled Build accounts after they prove they can think again.
// Zero Enabled leaves the scheduled worker idle; manual probes still run.
type DisabledAccountRevivalRuntime struct {
	Enabled        bool
	Interval       time.Duration
	BatchSize      int
	Concurrency    int
	MinDisabledAge time.Duration
	FailRetryAfter time.Duration
	Timeout        time.Duration
}

type DisabledAccountProbeItem struct {
	AccountID       uint64
	Name            string
	Outcome         string
	Reason          string
	OutputTokens    int64
	ReasoningTokens int64
}

type DisabledAccountProbeBatchResult struct {
	Revived  int
	Failed   int
	Skipped  int
	TimedOut int
}

type DisabledAccountProbeObserver func(completed, total int, item DisabledAccountProbeItem) error

func normalizeDisabledAccountRevival(cfg DisabledAccountRevivalRuntime) DisabledAccountRevivalRuntime {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultDisabledRevivalInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultDisabledRevivalBatchSize
	}
	if cfg.BatchSize > maxDisabledRevivalBatchSize {
		cfg.BatchSize = maxDisabledRevivalBatchSize
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultDisabledRevivalConcurrency
	}
	if cfg.Concurrency > maxDisabledRevivalConcurrency {
		cfg.Concurrency = maxDisabledRevivalConcurrency
	}
	if cfg.MinDisabledAge <= 0 {
		cfg.MinDisabledAge = defaultDisabledRevivalMinDisabledAge
	}
	if cfg.FailRetryAfter <= 0 {
		cfg.FailRetryAfter = defaultDisabledRevivalFailRetryAfter
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultDisabledRevivalTimeout
	}
	return cfg
}

func disabledRevivalEligible(credential accountdomain.Credential, now time.Time, cfg DisabledAccountRevivalRuntime) bool {
	if !disabledRevivalCandidate(credential) {
		return false
	}
	if credential.CooldownUntil != nil && now.Before(*credential.CooldownUntil) {
		return false
	}
	if cfg.MinDisabledAge > 0 && credential.LastUsedAt != nil && now.Sub(credential.LastUsedAt.UTC()) < cfg.MinDisabledAge {
		return false
	}
	return true
}

func disabledRevivalCandidate(credential accountdomain.Credential) bool {
	if credential.Enabled || credential.AuthStatus != accountdomain.AuthStatusActive {
		return false
	}
	if credential.Provider != accountdomain.ProviderBuild {
		return false
	}
	if isMissingThinkingStrike(credential.LastError) {
		return true
	}
	return credential.LastError == accountdomain.LastErrorThinkingProbation
}

func disabledManualProbeCandidate(credential accountdomain.Credential) bool {
	return disabledRevivalCandidate(credential)
}

func disabledRevivalTimeoutRetryAfter(cfg DisabledAccountRevivalRuntime) time.Duration {
	if cfg.MinDisabledAge > 0 {
		return cfg.MinDisabledAge
	}
	return defaultDisabledRevivalTimeoutRetryAfter
}

func disabledRevivalKeepMarker(credential accountdomain.Credential) string {
	if strings.TrimSpace(credential.LastError) != "" {
		return credential.LastError
	}
	return lastErrorMissingThinking
}

func disabledRevivalHasThinking(sig QualityStreamSignals) bool {
	if !sig.HasReasoningDelta && !sig.HasThinking {
		return false
	}
	return ClassifyQualityHold(sig, defaultQualityMinOutput) == QualityDeliver
}

// RunDisabledAccountRevival is the supervised background loop. It waits briefly
// after startup, then ticks every minute and no-ops until Interval has elapsed.
func (s *Service) RunDisabledAccountRevival(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if err := s.ReviveDisabledAccounts(ctx); err != nil && s.logger != nil {
		s.logger.Warn("disabled_account_revival_failed", "error", err)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
			err := s.ReviveDisabledAccounts(runCtx)
			cancel()
			if err != nil && s.logger != nil {
				s.logger.Warn("disabled_account_revival_failed", "error", err)
			}
		}
	}
}

// ReviveDisabledAccounts probes one batch of quality-disabled Build accounts.
// Accounts stay out of the live pool until thinking is observed.
func (s *Service) ReviveDisabledAccounts(ctx context.Context) error {
	if s == nil || s.selector == nil || s.selector.accounts == nil || s.clientKeys == nil {
		return nil
	}
	cfg := normalizeDisabledAccountRevival(s.qualityRetryConfig().DisabledRevival)
	if !cfg.Enabled {
		return nil
	}
	now := time.Now().UTC()
	s.disabledRevivalMu.Lock()
	if !s.disabledRevivalLastRun.IsZero() && now.Sub(s.disabledRevivalLastRun) < cfg.Interval {
		s.disabledRevivalMu.Unlock()
		return nil
	}
	s.disabledRevivalLastRun = now
	afterID := s.disabledRevivalAfterID
	s.disabledRevivalMu.Unlock()

	picked, nextAfterID, scanned, wrapped, err := s.collectDisabledRevivalBatch(ctx, afterID, now, cfg)
	if err != nil {
		return err
	}
	s.disabledRevivalMu.Lock()
	s.disabledRevivalAfterID = nextAfterID
	s.disabledRevivalMu.Unlock()
	if s.logger != nil {
		s.logger.Info("disabled_account_revival_scan", "scanned", scanned, "picked", len(picked), "after_id", nextAfterID, "wrapped", wrapped, "concurrency", cfg.Concurrency)
	}
	if len(picked) == 0 {
		return nil
	}
	result := s.probeDisabledAccountBatch(ctx, picked, cfg, nil)
	if s.logger != nil {
		s.logger.Info("disabled_account_revival_batch", "revived", result.Revived, "failed", result.Failed, "timed_out", result.TimedOut, "picked", len(picked), "concurrency", cfg.Concurrency)
	}
	return nil
}

// ProbeDisabledAccounts runs an on-demand thinking probe for selected IDs.
// It ignores the scheduled min-age and fail-retry backoff so an operator can
// retry immediately. Non-quality disables are skipped, not enabled.
func (s *Service) ProbeDisabledAccounts(ctx context.Context, ids []uint64, observer DisabledAccountProbeObserver) (DisabledAccountProbeBatchResult, error) {
	empty := DisabledAccountProbeBatchResult{}
	if s == nil || s.selector == nil || s.selector.accounts == nil || s.clientKeys == nil {
		return empty, errors.New("停用号探测未就绪")
	}
	if len(ids) == 0 {
		return empty, errors.New("必须提供账号 ID")
	}
	if len(ids) > maxDisabledRevivalManualIDs {
		return empty, fmt.Errorf("一次最多探测 %d 个账号", maxDisabledRevivalManualIDs)
	}
	cfg := normalizeDisabledAccountRevival(s.qualityRetryConfig().DisabledRevival)
	picked := make([]accountdomain.Credential, 0, len(ids))
	result := DisabledAccountProbeBatchResult{}
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			result.Skipped++
			continue
		}
		if _, exists := seen[id]; exists {
			result.Skipped++
			continue
		}
		seen[id] = struct{}{}
		credential, err := s.selector.accounts.Get(ctx, id)
		if err != nil {
			result.Failed++
			item := DisabledAccountProbeItem{AccountID: id, Outcome: "failed", Reason: err.Error()}
			if observer != nil {
				if obsErr := observer(result.Revived+result.Failed+result.Skipped, len(ids), item); obsErr != nil {
					return result, obsErr
				}
			}
			continue
		}
		if !disabledManualProbeCandidate(credential) {
			result.Skipped++
			reason := "not_quality_disabled"
			if credential.Enabled {
				reason = "already_enabled"
			} else if credential.AuthStatus != accountdomain.AuthStatusActive {
				reason = "reauth_required"
			} else if credential.Provider != accountdomain.ProviderBuild {
				reason = "not_build"
			}
			item := DisabledAccountProbeItem{AccountID: credential.ID, Name: credential.Name, Outcome: "skipped", Reason: reason}
			if observer != nil {
				if obsErr := observer(result.Revived+result.Failed+result.Skipped, len(ids), item); obsErr != nil {
					return result, obsErr
				}
			}
			continue
		}
		picked = append(picked, credential)
	}
	if len(picked) == 0 {
		if observer != nil {
			_ = observer(result.Revived+result.Failed+result.Skipped, len(ids), DisabledAccountProbeItem{})
		}
		return result, nil
	}
	already := result.Skipped + result.Failed
	probed := s.probeDisabledAccountBatch(ctx, picked, cfg, func(completed, _ int, item DisabledAccountProbeItem) error {
		if observer == nil {
			return nil
		}
		return observer(already+completed, len(ids), item)
	})
	result.Revived += probed.Revived
	result.Failed += probed.Failed
	return result, nil
}

func (s *Service) collectDisabledRevivalBatch(ctx context.Context, afterID uint64, now time.Time, cfg DisabledAccountRevivalRuntime) ([]accountdomain.Credential, uint64, int, bool, error) {
	picked := make([]accountdomain.Credential, 0, cfg.BatchSize)
	scanned := 0
	cursor := afterID
	wrapped := false
	for scanned < disabledRevivalScanLimit && len(picked) < cfg.BatchSize {
		batch, _, err := s.selector.accounts.ListProviderAccountBatch(ctx, accountdomain.ProviderBuild, cursor, 100)
		if err != nil {
			return nil, cursor, scanned, wrapped, err
		}
		if len(batch) == 0 {
			if cursor == 0 || wrapped {
				break
			}
			cursor = 0
			wrapped = true
			continue
		}
		for _, credential := range batch {
			cursor = credential.ID
			scanned++
			if disabledRevivalEligible(credential, now, cfg) {
				picked = append(picked, credential)
				if len(picked) >= cfg.BatchSize {
					break
				}
			}
			if scanned >= disabledRevivalScanLimit {
				break
			}
		}
	}
	return picked, cursor, scanned, wrapped, nil
}

func (s *Service) probeDisabledAccountBatch(ctx context.Context, picked []accountdomain.Credential, cfg DisabledAccountRevivalRuntime, observer DisabledAccountProbeObserver) DisabledAccountProbeBatchResult {
	result := DisabledAccountProbeBatchResult{}
	if len(picked) == 0 {
		return result
	}
	concurrency := cfg.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(picked) {
		concurrency = len(picked)
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var completed atomic.Int64
	total := len(picked)
	if observer != nil {
		_ = observer(0, total, DisabledAccountProbeItem{})
	}
	for _, credential := range picked {
		if ctx.Err() != nil {
			break
		}
		cred := credential
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			ok, inconclusive, usage, probeErr := s.reviveOneDisabledAccount(ctx, cred, cfg)
			item := DisabledAccountProbeItem{
				AccountID: cred.ID, Name: cred.Name, Outcome: "failed",
				OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens,
			}
			if ok {
				item.Outcome = "revived"
			} else if inconclusive {
				item.Outcome = "timeout"
				if probeErr != nil {
					item.Reason = probeErr.Error()
				}
			} else if probeErr != nil {
				item.Reason = probeErr.Error()
			}
			mu.Lock()
			if ok {
				result.Revived++
			} else if inconclusive {
				result.TimedOut++
			} else {
				result.Failed++
			}
			mu.Unlock()
			n := int(completed.Add(1))
			if observer != nil {
				_ = observer(n, total, item)
			}
		}()
	}
	wg.Wait()
	return result
}

func (s *Service) reviveOneDisabledAccount(ctx context.Context, credential accountdomain.Credential, cfg DisabledAccountRevivalRuntime) (bool, bool, Usage, error) {
	probeCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	hasThinking, usage, err := s.probeDisabledAccount(probeCtx, credential.ID)
	if err != nil {
		until := time.Now().UTC().Add(disabledRevivalTimeoutRetryAfter(cfg))
		if healthErr := s.selector.accounts.UpdateHealth(ctx, credential.ID, credential.Provider, credential.FailureCount, &until, disabledRevivalKeepMarker(credential), false); healthErr != nil && s.logger != nil {
			s.logger.Warn("disabled_account_probe_backoff_failed", "account_id", credential.ID, "error", healthErr)
		}
		return false, true, usage, err
	}
	if !hasThinking {
		until := time.Now().UTC().Add(cfg.FailRetryAfter)
		if healthErr := s.selector.accounts.UpdateHealth(ctx, credential.ID, credential.Provider, credential.FailureCount, &until, lastErrorMissingThinkingDisabled, false); healthErr != nil && s.logger != nil {
			s.logger.Warn("disabled_account_probe_backoff_failed", "account_id", credential.ID, "error", healthErr)
		}
		return false, false, usage, fmt.Errorf("missing thinking output=%d reasoning=%d", usage.OutputTokens, usage.ReasoningTokens)
	}
	enabled := true
	if _, updateErr := s.selector.accounts.UpdateMany(ctx, credential.Provider, []uint64{credential.ID}, repository.AccountUpdates{Enabled: &enabled}); updateErr != nil {
		return false, false, usage, updateErr
	}
	if healthErr := s.selector.accounts.UpdateHealth(ctx, credential.ID, credential.Provider, 0, nil, lastErrorThinkingProbation, true); healthErr != nil {
		return false, false, usage, healthErr
	}
	s.selector.MarkRevivedForLiveProbation(credential.ID)
	s.selector.ApplyInvalidation(repository.InvalidationEvent{
		Kind:         repository.InvalidationAccountStateChanged,
		Provider:     credential.Provider,
		AccountID:    credential.ID,
		HealthMarker: accountdomain.LastErrorThinkingProbation,
	})
	if s.logger != nil {
		s.logger.Info("disabled_account_revived", "account_id", credential.ID, "name", credential.Name, "output_tokens", usage.OutputTokens, "reasoning_tokens", usage.ReasoningTokens)
	}
	return true, false, usage, nil
}

func (s *Service) probeDisabledAccount(ctx context.Context, accountID uint64) (bool, Usage, error) {
	key, err := s.clientKeys.EnsureQualityGuardIdentity(ctx, true)
	if err != nil {
		return false, Usage{}, fmt.Errorf("读取停用号探测身份: %w", err)
	}
	if !key.IsAvailable(time.Now().UTC()) {
		return false, Usage{}, errors.New("停用号探测身份已禁用或过期")
	}
	requestIDPart, err := security.NewOpaqueToken(12)
	if err != nil {
		return false, Usage{}, err
	}
	publicModel, ok := modeldomain.NormalizePublicID(accountdomain.ProviderBuild, disabledRevivalModel)
	if !ok {
		return false, Usage{}, errors.New("停用号探测模型必须属于 Grok Build")
	}
	body, err := json.Marshal(map[string]any{
		"model":            disabledRevivalModel,
		"messages":         []map[string]string{{"role": "user", "content": disabledRevivalPrompt}},
		"stream":           true,
		"stream_options":   map[string]bool{"include_usage": true},
		"max_tokens":       disabledRevivalMaxOutputTokens,
		"reasoning_effort": "medium",
	})
	if err != nil {
		return false, Usage{}, err
	}
	result, err := s.CreateChatCompletion(ctx, Input{
		RequestID:           "revive_" + requestIDPart,
		ClientKey:           key,
		PublicModel:         publicModel,
		Body:                body,
		Streaming:           true,
		Operation:           audit.OperationChat,
		ForcedAccountID:     accountID,
		skipQualityHold:     true,
		accountRevivalProbe: true,
	})
	if err != nil {
		return false, Usage{}, err
	}
	defer result.Body.Close()
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(result.Body, 32<<10))
		return false, Usage{}, fmt.Errorf("停用号探测上游返回 %d: %s", result.StatusCode, strings.TrimSpace(string(body)))
	}

	holdCfg := s.qualityRetryConfig()
	state := qualityScanState{
		protocol:                        qualityProtocolChat,
		minEncryptedBytes:               holdCfg.MinEncryptedBytes,
		encryptedBytesPerReasoningToken: holdCfg.EncryptedBytesPerReasoningToken,
	}
	scanner := bufio.NewScanner(result.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	totalBytes := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		totalBytes += len(line) + 1
		if totalBytes > qualityProbeMaxStreamBytes {
			return false, state.usage, errors.New("停用号探测响应过大")
		}
		ObserveQualityChunk(&state, append(append([]byte(nil), bytes.TrimSpace(line)...), '\n'))
	}
	if err := scanner.Err(); err != nil {
		return false, state.usage, fmt.Errorf("读取停用号探测流: %w", err)
	}
	state.terminal = true
	return disabledRevivalHasThinking(state.signals()), state.usage, nil
}
