package asynqx

import (
	"context"
	"sync"
	"sync/atomic"
)

// 生命周期状态机的五个状态，Worker 与 Scheduler 共用。
const (
	stateIdle int32 = iota
	stateStarting
	stateRunning
	stateStopping
	stateStopped
)

// lifecycle 封装 Worker 与 Scheduler 共享的启动/关闭状态机：
// CAS 驱动的状态迁移、并发 Start/Shutdown 竞态处理、幂等停止与停止等待。
// 通过嵌入使用；调用 init 完成初始化后才可使用其余方法。
type lifecycle struct {
	state       atomic.Int32
	stopped     chan struct{}
	stoppedOnce sync.Once
	stopOnce    sync.Once

	// errAlreadyRunning / errStopped 是宿主组件语义化的 sentinel error，
	// 分别在重复启动与已停止时返回。
	errAlreadyRunning error
	errStopped        error

	// stopFn 执行宿主真正的资源关闭（如 runner.Shutdown、等待在途操作），
	// 由 finishStop 恰好调用一次。
	stopFn func()
}

func (l *lifecycle) init(errAlreadyRunning, errStopped error, stopFn func()) {
	l.stopped = make(chan struct{})
	l.errAlreadyRunning = errAlreadyRunning
	l.errStopped = errStopped
	l.stopFn = stopFn
}

// start 执行启动流程：CAS 进入 starting 后依次执行各 step，全部成功则进入 running。
// 任一 step 失败时把状态回滚为 idle；若期间已有并发 Shutdown 把状态改为 stopping，
// 则接手执行停止流程并返回 errStopped，绝不覆盖 stopping（避免 Shutdown 永久阻塞）。
func (l *lifecycle) start(steps ...func() error) error {
	if !l.state.CompareAndSwap(stateIdle, stateStarting) {
		switch l.state.Load() {
		case stateStopping, stateStopped:
			return l.errStopped
		default:
			return l.errAlreadyRunning
		}
	}

	for _, step := range steps {
		err := step()
		if err == nil {
			continue
		}

		if l.state.CompareAndSwap(stateStarting, stateIdle) {
			return err
		}

		if l.state.Load() == stateStopping {
			l.beginStop(false)

			return l.errStopped
		}

		return err
	}

	if l.state.CompareAndSwap(stateStarting, stateRunning) {
		return nil
	}

	if l.state.Load() == stateStopping {
		l.beginStop(false)

		return l.errStopped
	}

	return nil
}

// shutdown 请求停止并等待停止完成或 ctx 取消。重复调用安全。
// idle 态下同步执行停止并立即返回；starting 态下由 start 一方在观察到 stopping
// 后接手执行停止；running 态下异步执行停止、本方法等待其完成。
func (l *lifecycle) shutdown(ctx context.Context) error {
	for {
		switch l.state.Load() {
		case stateIdle:
			if l.state.CompareAndSwap(stateIdle, stateStopping) {
				l.beginStop(false)

				return nil
			}
		case stateStarting:
			if l.state.CompareAndSwap(stateStarting, stateStopping) {
				return l.waitStopped(ctx)
			}
		case stateRunning:
			if l.state.CompareAndSwap(stateRunning, stateStopping) {
				l.beginStop(true)

				return l.waitStopped(ctx)
			}
		default: // stateStopping / stateStopped：停止已在进行，仅等待。
			return l.waitStopped(ctx)
		}
	}
}

func (l *lifecycle) beginStop(async bool) {
	l.stopOnce.Do(func() {
		if async {
			go l.finishStop()

			return
		}

		l.finishStop()
	})
}

func (l *lifecycle) finishStop() {
	l.stopFn()
	l.markStopped()
}

func (l *lifecycle) markStopped() {
	l.state.Store(stateStopped)
	l.stoppedOnce.Do(func() {
		close(l.stopped)
	})
}

func (l *lifecycle) waitStopped(ctx context.Context) error {
	if l.state.Load() == stateStopped {
		return nil
	}

	select {
	case <-l.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
