package asynqx

import (
	"fmt"
	"strings"
	"time"

	"github.com/gtkit/json"
	"github.com/hibiken/asynq"
)

const taskOptionsCapacity = 10

// TaskOption 表示任务投递时的可选参数。
type TaskOption func(*taskOptions) error

type taskOptions struct {
	queue        string
	hasQueue     bool
	group        string
	hasGroup     bool
	timeout      time.Duration
	hasTimeout   bool
	deadline     time.Time
	hasDeadline  bool
	delay        time.Duration
	hasDelay     bool
	processAt    time.Time
	hasProcessAt bool
	maxRetry     int
	hasMaxRetry  bool
	uniqueTTL    time.Duration
	hasUnique    bool
	retention    time.Duration
	hasRetention bool
	taskID       string
	hasTaskID    bool
	raw          []asynq.Option
}

// WithTaskQueue 设置任务队列名。
func WithTaskQueue(queue string) TaskOption {
	return func(opts *taskOptions) error {
		if strings.TrimSpace(queue) == "" {
			return invalidTaskOptionError("queue", "must not be empty")
		}

		opts.queue = queue
		opts.hasQueue = true

		return nil
	}
}

// WithTaskGroup 设置任务聚合分组名。
func WithTaskGroup(group string) TaskOption {
	return func(opts *taskOptions) error {
		if strings.TrimSpace(group) == "" {
			return invalidTaskOptionError("group", "must not be empty")
		}

		opts.group = group
		opts.hasGroup = true

		return nil
	}
}

// WithTaskTimeout 设置任务超时时间。
func WithTaskTimeout(timeout time.Duration) TaskOption {
	return func(opts *taskOptions) error {
		if timeout < 0 {
			return invalidTaskOptionError("timeout", "must be >= 0")
		}

		opts.timeout = timeout
		opts.hasTimeout = true

		return nil
	}
}

// WithTaskDeadline 设置任务截止时间。
func WithTaskDeadline(deadline time.Time) TaskOption {
	return func(opts *taskOptions) error {
		if deadline.IsZero() {
			return invalidTaskOptionError("deadline", "must not be zero")
		}

		opts.deadline = deadline
		opts.hasDeadline = true

		return nil
	}
}

// WithTaskDelay 设置相对当前时间的延迟投递时间。
// 如果同一次投递同时配置了 WithTaskProcessAt，则后应用的调度选项会覆盖先前的设置。
func WithTaskDelay(delay time.Duration) TaskOption {
	return func(opts *taskOptions) error {
		if delay < 0 {
			return invalidTaskOptionError("delay", "must be >= 0")
		}

		opts.delay = delay
		opts.hasDelay = true
		opts.processAt = time.Time{}
		opts.hasProcessAt = false

		return nil
	}
}

// WithTaskProcessAt 设置任务的绝对投递时间。
// 如果同一次投递同时配置了 WithTaskDelay，则后应用的调度选项会覆盖先前的设置。
func WithTaskProcessAt(processAt time.Time) TaskOption {
	return func(opts *taskOptions) error {
		if processAt.IsZero() {
			return invalidTaskOptionError("process_at", "must not be zero")
		}

		opts.processAt = processAt
		opts.hasProcessAt = true
		opts.delay = 0
		opts.hasDelay = false

		return nil
	}
}

// WithTaskMaxRetry 设置任务最大重试次数。
func WithTaskMaxRetry(maxRetry int) TaskOption {
	return func(opts *taskOptions) error {
		if maxRetry < 0 {
			return invalidTaskOptionError("max_retry", "must be >= 0")
		}

		opts.maxRetry = maxRetry
		opts.hasMaxRetry = true

		return nil
	}
}

// WithTaskUnique 设置任务唯一性窗口。
func WithTaskUnique(ttl time.Duration) TaskOption {
	return func(opts *taskOptions) error {
		if ttl < time.Second {
			return invalidTaskOptionError("unique", "must be >= 1s")
		}

		opts.uniqueTTL = ttl
		opts.hasUnique = true

		return nil
	}
}

// WithTaskRetention 设置任务完成后的保留时长。
func WithTaskRetention(retention time.Duration) TaskOption {
	return func(opts *taskOptions) error {
		if retention <= 0 {
			return invalidTaskOptionError("retention", "must be > 0")
		}

		opts.retention = retention
		opts.hasRetention = true

		return nil
	}
}

// WithTaskID 设置任务 ID。
func WithTaskID(taskID string) TaskOption {
	return func(opts *taskOptions) error {
		if strings.TrimSpace(taskID) == "" {
			return invalidTaskOptionError("task_id", "must not be empty")
		}

		opts.taskID = taskID
		opts.hasTaskID = true

		return nil
	}
}

// WithTaskRawOptions 透传原生 asynq.Option，用于投递 asynqx 尚未镜像的能力。
// 透传选项在镜像选项之后应用，与 asynq「后者覆盖前者」的语义一致，
// 因此可用它覆盖由 WithTaskQueue 等生成的选项；其中的超时/截止选项也会被默认超时注入逻辑识别。
func WithTaskRawOptions(opts ...asynq.Option) TaskOption {
	return func(resolved *taskOptions) error {
		for i, opt := range opts {
			if opt == nil {
				return invalidTaskOptionError(fmt.Sprintf("raw_options[%d]", i), "must not be nil")
			}
		}

		resolved.raw = append(resolved.raw, opts...)

		return nil
	}
}

func marshalPayload(payload any) ([]byte, error) {
	return json.Marshal(payload)
}

func buildTaskOptions(opts ...TaskOption) ([]asynq.Option, error) {
	var resolved taskOptions

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		err := opt(&resolved)
		if err != nil {
			return nil, err
		}
	}

	built := make([]asynq.Option, 0, taskOptionsCapacity)
	if resolved.hasQueue {
		built = append(built, asynq.Queue(resolved.queue))
	}

	if resolved.hasGroup {
		built = append(built, asynq.Group(resolved.group))
	}

	if resolved.hasTimeout {
		built = append(built, asynq.Timeout(resolved.timeout))
	}

	if resolved.hasDeadline {
		built = append(built, asynq.Deadline(resolved.deadline))
	}

	if resolved.hasProcessAt {
		built = append(built, asynq.ProcessAt(resolved.processAt))
	} else if resolved.hasDelay {
		built = append(built, asynq.ProcessIn(resolved.delay))
	}

	if resolved.hasMaxRetry {
		built = append(built, asynq.MaxRetry(resolved.maxRetry))
	}

	if resolved.hasUnique {
		built = append(built, asynq.Unique(resolved.uniqueTTL))
	}

	if resolved.hasRetention {
		built = append(built, asynq.Retention(resolved.retention))
	}

	if resolved.hasTaskID {
		built = append(built, asynq.TaskID(resolved.taskID))
	}

	if len(resolved.raw) > 0 {
		built = append(built, resolved.raw...)
	}

	return built, nil
}

func applyDefaultTaskTimeout(opts []asynq.Option, timeout time.Duration) []asynq.Option {
	if timeout <= 0 {
		return opts
	}

	for _, opt := range opts {
		if opt.Type() == asynq.TimeoutOpt || opt.Type() == asynq.DeadlineOpt {
			return opts
		}
	}

	return append(opts, asynq.Timeout(timeout))
}
