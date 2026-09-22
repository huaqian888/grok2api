package gateway

import (
	"testing"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
)

func TestDisabledRevivalEligibleOnlyQualityDisabledBuildAccounts(t *testing.T) {
	now := time.Now().UTC()
	used := now.Add(-2 * time.Hour)
	cfg := normalizeDisabledAccountRevival(DisabledAccountRevivalRuntime{Enabled: true})
	ok := accountdomain.Credential{
		ID: 1, Provider: accountdomain.ProviderBuild, Enabled: false, AuthStatus: accountdomain.AuthStatusActive,
		LastError: accountdomain.LastErrorMissingThinkingDisabled, LastUsedAt: &used,
	}
	if !disabledRevivalEligible(ok, now, cfg) {
		t.Fatal("quality-disabled active account must be eligible")
	}
	enabled := ok
	enabled.Enabled = true
	if disabledRevivalEligible(enabled, now, cfg) {
		t.Fatal("enabled accounts must not be probed")
	}
	reauth := ok
	reauth.AuthStatus = accountdomain.AuthStatusReauthRequired
	if disabledRevivalEligible(reauth, now, cfg) {
		t.Fatal("reauth accounts must not be probed")
	}
	manual := ok
	manual.LastError = "admin_disabled"
	if disabledRevivalEligible(manual, now, cfg) {
		t.Fatal("non-quality disables must stay disabled")
	}
	recent := ok
	recentUsed := now.Add(-time.Minute)
	recent.LastUsedAt = &recentUsed
	if disabledRevivalEligible(recent, now, cfg) {
		t.Fatal("freshly disabled accounts must wait minDisabledAge")
	}
	cooling := ok
	until := now.Add(time.Hour)
	cooling.CooldownUntil = &until
	if disabledRevivalEligible(cooling, now, cfg) {
		t.Fatal("failed-probe backoff must skip the account")
	}
	probation := ok
	probation.LastError = accountdomain.LastErrorThinkingProbation
	if !disabledRevivalEligible(probation, now, cfg) {
		t.Fatal("thinking_probation disabled accounts must be eligible for scheduled revival")
	}
	if !disabledManualProbeCandidate(probation) {
		t.Fatal("thinking_probation must be probeable from the admin button")
	}
	if disabledRevivalTimeoutRetryAfter(cfg) != defaultDisabledRevivalMinDisabledAge {
		t.Fatalf("timeout retry = %s", disabledRevivalTimeoutRetryAfter(cfg))
	}
	if got := disabledRevivalKeepMarker(probation); got != accountdomain.LastErrorThinkingProbation {
		t.Fatalf("timeout must keep thinking_probation, got %q", got)
	}
}

func TestNormalizeDisabledAccountRevivalDefaults(t *testing.T) {
	got := normalizeDisabledAccountRevival(DisabledAccountRevivalRuntime{Enabled: true, BatchSize: 250, Concurrency: 99})
	if !got.Enabled || got.Interval != defaultDisabledRevivalInterval || got.BatchSize != maxDisabledRevivalBatchSize || got.Concurrency != maxDisabledRevivalConcurrency || got.MinDisabledAge != defaultDisabledRevivalMinDisabledAge || got.FailRetryAfter != defaultDisabledRevivalFailRetryAfter || got.Timeout != defaultDisabledRevivalTimeout {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	zero := normalizeDisabledAccountRevival(DisabledAccountRevivalRuntime{Enabled: true})
	if zero.BatchSize != defaultDisabledRevivalBatchSize || zero.Concurrency != defaultDisabledRevivalConcurrency {
		t.Fatalf("zero defaults: %+v", zero)
	}
	kept := normalizeDisabledAccountRevival(DisabledAccountRevivalRuntime{Enabled: true, BatchSize: 40, Concurrency: 32})
	if kept.Concurrency != 32 || kept.BatchSize != 40 {
		t.Fatalf("concurrency must not be clamped to batchSize: %+v", kept)
	}
}

func TestDisabledRevivalHasThinkingRequiresReasoningEvidence(t *testing.T) {
	if disabledRevivalHasThinking(QualityStreamSignals{VisibleTokens: 40, OutputTokens: 40, Terminal: true}) {
		t.Fatal("visible dump without thinking must not revive")
	}
	if disabledRevivalHasThinking(QualityStreamSignals{HasThinking: true, EncryptedBytes: 300}) {
		t.Fatal("non-terminal ciphertext-only thinking must wait, not revive")
	}
	if !disabledRevivalHasThinking(QualityStreamSignals{HasReasoningDelta: true, VisibleTokens: 8}) {
		t.Fatal("reasoning delta must revive")
	}
	if !disabledRevivalHasThinking(QualityStreamSignals{
		HasThinking: true, EncryptedBytes: 400, EncryptedFloor: 256, Terminal: true, UsageReported: true, VisibleTokens: 20,
	}) {
		t.Fatal("terminal classified thinking must revive")
	}
	if disabledRevivalHasThinking(QualityStreamSignals{
		HasThinking: true, EncryptedBytes: 4000, EncryptedFloor: 256, Terminal: true, HoldExpired: true,
		VisibleTokens: 3, ReasoningTokens: 954, FirstVisible: true,
	}) {
		t.Fatal("burst dump after hold must not revive")
	}
}
