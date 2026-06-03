package asynqx

import (
	"math/rand/v2"
	"time"

	"github.com/hibiken/asynq"
)

// backoffFactor 是指数退避每次重试的增长倍数，同时用作等量抖动（equal jitter）的二分基数。
const backoffFactor = 2

// CappedExponentialBackoff 返回一个带上限和抖动的指数退避 RetryDelayFunc，
// 可通过 WithRetryDelayFunc 注入。
//
// asynq 默认的 DefaultRetryDelayFunc 已是指数退避加抖动，但其延迟随重试次数以 n^4
// 无上限增长（默认最多 25 次重试时末次延迟可达数天）。当需要把单次重试延迟封顶
// （例如指数退避但最多只等 1 小时）时使用本函数。
//
// 退避按 base、2*base、4*base …… 增长并在超过 maxDelay 后封顶；
// 每次在 [delay/2, delay] 区间内取等量抖动，避免大量任务在同一时刻集中重试。
// 当 base 或 maxDelay <= 0 时返回 0（立即重试）；当 maxDelay < base 时延迟被 maxDelay 封顶。
func CappedExponentialBackoff(base, maxDelay time.Duration) asynq.RetryDelayFunc {
	return func(retried int, _ error, _ *asynq.Task) time.Duration {
		if base <= 0 || maxDelay <= 0 {
			return 0
		}

		backoff := base

		for range retried {
			if backoff > maxDelay/backoffFactor {
				backoff = maxDelay

				break
			}

			backoff *= backoffFactor
		}

		if backoff > maxDelay {
			backoff = maxDelay
		}

		half := backoff / backoffFactor

		// 弱随机已满足抖动需求，无需加密强度随机。
		return half + time.Duration(rand.Int64N(int64(half)+1)) //nolint:gosec // G404: jitter does not need crypto randomness
	}
}
