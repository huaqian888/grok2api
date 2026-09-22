package account

import (
	"context"
	"net/http"
	"strconv"

	"github.com/chenyme/grok2api/backend/internal/application/gateway"
	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

type disabledAccountProber interface {
	ProbeDisabledAccounts(ctx context.Context, ids []uint64, observer gateway.DisabledAccountProbeObserver) (gateway.DisabledAccountProbeBatchResult, error)
}

type probeQualityDisabledRequest struct {
	Provider string   `json:"provider"`
	IDs      []string `json:"ids"`
}

func (h *Handler) WithDisabledAccountProber(prober disabledAccountProber) *Handler {
	if h != nil {
		h.prober = prober
	}
	return h
}

func (h *Handler) probeQualityDisabled(c *gin.Context) {
	if h == nil || h.prober == nil {
		response.Error(c, http.StatusServiceUnavailable, "qualityProbeUnavailable", "停用号探测未就绪")
		return
	}
	var request probeQualityDisabledRequest
	if c.ShouldBindJSON(&request) != nil {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "请求参数无效")
		return
	}
	if request.Provider != "" && request.Provider != string(accountdomain.ProviderBuild) {
		response.Error(c, http.StatusBadRequest, "invalidProvider", "仅 Grok Build 账号支持降智探测")
		return
	}
	ids, err := parseIDs(request.IDs)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalidId", err.Error())
		return
	}
	if len(ids) == 0 {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "必须提供非空账号 ID")
		return
	}
	if !h.validateProviderIDs(c, ids, string(accountdomain.ProviderBuild)) {
		return
	}
	stream := newAccountEventStream(c)
	defer stream.Close()
	result, probeErr := h.prober.ProbeDisabledAccounts(c.Request.Context(), ids, func(completed, total int, item gateway.DisabledAccountProbeItem) error {
		if writeErr := stream.Write("progress", accountTaskProgressResponse{Completed: completed, Total: total}); writeErr != nil {
			return writeErr
		}
		if item.AccountID == 0 {
			return nil
		}
		return stream.Write("item", gin.H{
			"id":              strconv.FormatUint(item.AccountID, 10),
			"name":            item.Name,
			"outcome":         item.Outcome,
			"reason":          item.Reason,
			"outputTokens":    item.OutputTokens,
			"reasoningTokens": item.ReasoningTokens,
		})
	})
	if probeErr != nil {
		stream.WriteError("accountQualityProbeFailed", probeErr.Error())
		return
	}
	_ = stream.Write("complete", gin.H{"succeeded": result.Revived, "failed": result.Failed, "skipped": result.Skipped, "timedOut": result.TimedOut})
}
