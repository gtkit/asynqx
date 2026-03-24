package asynqx

import (
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestMarshalPayloadRejectsUnsupportedType(t *testing.T) {
	_, err := marshalPayload(func() {})
	if err == nil {
		t.Fatal("expected error for unsupported payload type")
	}
}

func TestBuildTaskOptionsBuildsAsynqOptions(t *testing.T) {
	now := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)

	opts, err := buildTaskOptions(
		WithTaskQueue("critical"),
		WithTaskTimeout(2*time.Minute),
		WithTaskDeadline(now.Add(5*time.Minute)),
		WithTaskDelay(30*time.Second),
		WithTaskMaxRetry(7),
		WithTaskUnique(2*time.Minute),
		WithTaskRetention(10*time.Minute),
		WithTaskID("task-123"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertOptionSet(t, opts, map[asynq.OptionType]any{
		asynq.QueueOpt:     "critical",
		asynq.TimeoutOpt:   2 * time.Minute,
		asynq.DeadlineOpt:  now.Add(5 * time.Minute),
		asynq.ProcessInOpt: 30 * time.Second,
		asynq.MaxRetryOpt:  7,
		asynq.UniqueOpt:    2 * time.Minute,
		asynq.RetentionOpt: 10 * time.Minute,
		asynq.TaskIDOpt:    "task-123",
	})
}

func TestBuildTaskOptionsRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		opts []TaskOption
	}{
		{
			name: "empty queue",
			opts: []TaskOption{WithTaskQueue(" ")},
		},
		{
			name: "negative timeout",
			opts: []TaskOption{WithTaskTimeout(-time.Second)},
		},
		{
			name: "zero deadline",
			opts: []TaskOption{WithTaskDeadline(time.Time{})},
		},
		{
			name: "negative delay",
			opts: []TaskOption{WithTaskDelay(-time.Second)},
		},
		{
			name: "negative retry",
			opts: []TaskOption{WithTaskMaxRetry(-1)},
		},
		{
			name: "short unique ttl",
			opts: []TaskOption{WithTaskUnique(500 * time.Millisecond)},
		},
		{
			name: "zero retention",
			opts: []TaskOption{WithTaskRetention(0)},
		},
		{
			name: "empty task id",
			opts: []TaskOption{WithTaskID("")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildTaskOptions(tc.opts...)
			if err == nil {
				t.Fatal("expected invalid task option error")
			}
			if !errors.Is(err, ErrInvalidTaskOption) {
				t.Fatalf("expected ErrInvalidTaskOption, got %v", err)
			}
		})
	}
}

func TestBuildTaskOptionsProcessAtOverridesDelay(t *testing.T) {
	processAt := time.Date(2026, 3, 25, 11, 0, 0, 0, time.UTC)

	opts, err := buildTaskOptions(
		WithTaskDelay(time.Minute),
		WithTaskProcessAt(processAt),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(opts) != 1 {
		t.Fatalf("expected 1 option, got %d", len(opts))
	}

	assertAsynqOption(t, opts[0], asynq.ProcessAtOpt, processAt)
}

func TestBuildTaskOptionsDelayOverridesProcessAt(t *testing.T) {
	opts, err := buildTaskOptions(
		WithTaskProcessAt(time.Date(2026, 3, 25, 11, 0, 0, 0, time.UTC)),
		WithTaskDelay(time.Minute),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(opts) != 1 {
		t.Fatalf("expected 1 option, got %d", len(opts))
	}

	assertAsynqOption(t, opts[0], asynq.ProcessInOpt, time.Minute)
}

func assertAsynqOption(t *testing.T, opt asynq.Option, wantType asynq.OptionType, wantValue any) {
	t.Helper()

	if opt.Type() != wantType {
		t.Fatalf("expected option type %v, got %v", wantType, opt.Type())
	}
	if opt.Value() != wantValue {
		t.Fatalf("expected option value %#v, got %#v", wantValue, opt.Value())
	}
}

func assertOptionSet(t *testing.T, opts []asynq.Option, want map[asynq.OptionType]any) {
	t.Helper()

	if len(opts) != len(want) {
		t.Fatalf("expected %d options, got %d", len(want), len(opts))
	}

	got := make(map[asynq.OptionType]any, len(opts))
	for _, opt := range opts {
		got[opt.Type()] = opt.Value()
	}

	for optionType, wantValue := range want {
		value, ok := got[optionType]
		if !ok {
			t.Fatalf("expected option type %v to exist", optionType)
		}
		if value != wantValue {
			t.Fatalf("expected option value %#v for type %v, got %#v", wantValue, optionType, value)
		}
	}
}
