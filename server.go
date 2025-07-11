package asynqx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gtkit/json"

	"github.com/hibiken/asynq"
)

type Server struct {
	sync.RWMutex
	started bool

	baseCtx context.Context
	err     error

	asynqServer    *asynq.Server    // asynq server
	asynqClient    *asynq.Client    // asynq client
	asynqScheduler *asynq.Scheduler // asynq scheduler
	asynqInspector *asynq.Inspector // asynq inspector

	mux           *asynq.ServeMux      // asynq serve mux
	asynqConfig   asynq.Config         // asynq config
	redisOpt      asynq.RedisClientOpt // redis client option
	schedulerOpts *asynq.SchedulerOpts // scheduler options

	// 记录每个任务的唯一ID，用于后续查询任务的结果
	entryIDs    map[string]string
	mtxEntryIDs sync.RWMutex

	taskTimeout time.Duration // 任务超时时间
}

func NewServer(opts ...ServerOption) *Server {
	srv := &Server{
		baseCtx: context.Background(),
		started: false,

		redisOpt: asynq.RedisClientOpt{
			Addr:     defaultRedisAddress,
			Password: "",
			DB:       defaultRedisDB,
		},
		asynqConfig: asynq.Config{
			Concurrency: defaultConcurrency,
		},
		schedulerOpts: &asynq.SchedulerOpts{},
		mux:           asynq.NewServeMux(),

		entryIDs:    make(map[string]string),
		mtxEntryIDs: sync.RWMutex{},

		taskTimeout: defaultTaskTimeout,
	}

	srv.init(opts...)

	return srv
}

func (s *Server) init(opts ...ServerOption) {
	for _, o := range opts {
		o(s)
	}
	if logger == nil {
		initLogger(s)
	}
	var err error
	// 创建 asynq server
	if err = s.createAsynqServer(); err != nil {
		s.err = err
		logger.Error("create asynq server failed:", err)
	}

	// 创建 asynq client
	if err = s.createAsynqClient(); err != nil {
		s.err = err
		logger.Error("create asynq client failed:", err)
	}

	// 创建 asynq 定时任务
	if err = s.createAsynqScheduler(); err != nil {
		s.err = err
		logger.Error("create asynq scheduler failed:", err)
	}

	// Inspector是一个客户端接口，用于检查和更改 队列和任务.
	// inspector.CancelProcessing(id) 可取消指定 任务id 的任务.
	if err = s.createAsynqInspector(); err != nil {
		s.err = err
		logger.Error("create asynq inspector failed:", err)
	}
}

func (s *Server) Name() string {
	return "asynq"
}

// RegisterSubscriber register task subscriber.
func (s *Server) RegisterSubscriber(taskType string, msgHandler MsgHandler, binder Binder) error {
	return s.handleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
		handlerErr := make(chan error, 1)
		go func(t *asynq.Task) {
			var payload MsgPayload // any type
			if binder != nil {
				payload = binder()
			} else {
				payload = t.Payload()
			}

			if err := json.Unmarshal(t.Payload(), payload); err != nil {
				logger.Error("unmarshal Msg failed: ", err)
				handlerErr <- err
				return
			}

			// 调用具体的处理函数
			if err := msgHandler(t.Type(), payload); err != nil {
				logger.Error("handle Msg failed: ", err)
				handlerErr <- err
				return
			}
			handlerErr <- nil
		}(task)
		select {
		case err := <-handlerErr:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.taskTimeout):
			return fmt.Errorf("task timeout: %s", s.taskTimeout)
		}
	})
}

// RegisterSubscriber register task subscriber
// handler: func(string, MsgPayload) error 具体处理任务的函数
func RegisterSubscriber[T any](srv *Server, taskType string, taskHandler TaskHandler[T]) error {
	msgHandler := func(taskType string, payload MsgPayload) error {
		switch pl := payload.(type) {
		case *T:
			return taskHandler(taskType, pl)
		default:
			logger.Errorf("invalid payload struct type: %T\n", pl)
			return errors.New("invalid payload struct type")
		}
	}
	binder := func() any {
		return new(T)
	}
	return srv.RegisterSubscriber(
		// 任务名称
		taskType,
		// msgHandler: func(string, MsgPayload) error 调用处理任务内容的具体函数
		msgHandler,
		// binder: func() any 绑定任务参数的结构体
		binder,
	)
}

// RegisterSubscriberWithCtx register task subscriber with context.
func (s *Server) RegisterSubscriberWithCtx(taskType string, msgHandler MsgHandlerWithCtx, binder Binder) error {
	handler := func(ctx context.Context, task *asynq.Task) error {
		handlerErr := make(chan error, 1)
		go func(t *asynq.Task) {
			var payload MsgPayload
			if binder != nil {
				payload = binder()
			} else {
				payload = t.Payload()
			}

			if err := json.Unmarshal(t.Payload(), payload); err != nil {
				logger.Errorf("unmarshal Msg failed: %s", err)
				handlerErr <- err
				return
			}

			if err := msgHandler(ctx, t.Type(), payload); err != nil {
				logger.Errorf("handle Msg failed: %s", err)
				handlerErr <- err
				return
			}
			handlerErr <- nil
		}(task)

		select {
		case err := <-handlerErr:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.taskTimeout):
			return fmt.Errorf("task timeout: %s", s.taskTimeout)
		}
	}

	return s.handleFunc(taskType, handler)
}

// RegisterSubscriberWithCtx register task subscriber with context.
func RegisterSubscriberWithCtx[T any](srv *Server, taskType string, taskHandler TaskHandlerWithCtx[T]) error {
	return srv.RegisterSubscriberWithCtx(
		// 参数1: 任务名称
		taskType,

		// 参数2: msgHandler: func(ctx context.Context, taskType string, payload MsgPayload) error 调用处理任务内容的具体函数
		func(ctx context.Context, taskType string, payload MsgPayload) error {
			switch pl := payload.(type) {
			case *T:
				return taskHandler(ctx, taskType, pl)
			default:
				logger.Errorf("invalid payload struct type: %T\n", pl)
				return errors.New("invalid payload struct type")
			}
		},

		// 参数3: binder: func() any {var t T; return &t} 绑定任务参数的结构体.
		// 动态地创建一个与任务消息类型匹配的结构体实例, 用于接收和解析任务消息的内容.
		func() any {
			return new(T)
		},
	)
}

func (s *Server) handleFunc(pattern string, handler func(context.Context, *asynq.Task) error) error {
	if s.started {
		logger.Errorf("handleFunc [%s] failed", pattern)
		return errors.New("cannot handle func, server already started")
	}
	s.mux.HandleFunc(pattern, handler)
	return nil
}

// NewTask enqueue a new task.
func (s *Server) NewTask(typeName string, msg any, opts ...asynq.Option) error {
	if s.asynqClient == nil {
		if err := s.createAsynqClient(); err != nil {
			return err
		}
	}

	var err error

	var payload []byte
	if payload, err = json.Marshal(msg); err != nil {
		return err
	}

	task := asynq.NewTask(typeName, payload, opts...)
	if task == nil {
		return errors.New("new task failed")
	}

	taskInfo, err := s.asynqClient.Enqueue(task, opts...)
	if err != nil {
		logger.Errorf("[%s] Enqueue failed: %s", typeName, err.Error())
		return err
	}

	logger.Debugf("[%s] enqueued task: id=%s queue=%s", typeName, taskInfo.ID, taskInfo.Queue)

	return nil
}

// NewWaitResultTask enqueue a new task and wait for the result.
func (s *Server) NewWaitResultTask(typeName string, msg any, opts ...asynq.Option) error {
	if s.asynqClient == nil {
		if err := s.createAsynqClient(); err != nil {
			return err
		}
	}

	var err error

	var payload []byte
	if payload, err = json.Marshal(msg); err != nil {
		return err
	}

	task := asynq.NewTask(typeName, payload, opts...)
	if task == nil {
		return errors.New("new task failed")
	}

	taskInfo, err := s.asynqClient.Enqueue(task, opts...)
	if err != nil {
		logger.Errorf("[%s] Enqueue failed: %s", typeName, err.Error())
		return err
	}

	if s.asynqInspector == nil {
		if err = s.createAsynqInspector(); err != nil {
			return err
		}
	}

	_, err = waitResult(s.asynqInspector, taskInfo)
	if err != nil {
		logger.Errorf("[%s] wait result failed: %s", typeName, err.Error())
		return err
	}

	logger.Debugf("[%s] enqueued task: id=%s queue=%s", typeName, taskInfo.ID, taskInfo.Queue)

	return nil
}

func waitResult(intor *asynq.Inspector, info *asynq.TaskInfo) (*asynq.TaskInfo, error) {
	taskInfo, err := intor.GetTaskInfo(info.Queue, info.ID)
	if err != nil {
		return nil, err
	}

	if taskInfo.State != asynq.TaskStateCompleted && taskInfo.State != asynq.TaskStateArchived && taskInfo.State != asynq.TaskStateRetry { //nolint:lll
		return waitResult(intor, info)
	}

	if taskInfo.State == asynq.TaskStateRetry {
		return nil, fmt.Errorf("task state is %s", taskInfo.State.String())
	}

	return taskInfo, nil
}

// NewPeriodicTask enqueue a new crontab task.
func (s *Server) NewPeriodicTask(cronSpec, typeName string, msg any, opts ...asynq.Option) (string, error) {
	if s.asynqScheduler == nil {
		if err := s.createAsynqScheduler(); err != nil {
			return "", err
		}
		if err := s.runAsynqScheduler(); err != nil {
			return "", err
		}
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		logger.Errorf("[%s] Marshal payload failed: %s", typeName, err.Error())
		return "", err
	}

	task := asynq.NewTask(typeName, payload, opts...)
	if task == nil {
		return "", errors.New("new task failed")
	}

	entryID, err := s.asynqScheduler.Register(cronSpec, task, opts...)
	if err != nil {
		logger.Errorf("[%s] enqueue periodic task failed: %s", typeName, err.Error())
		return "", err
	}

	s.addPeriodicTaskEntryID(typeName, entryID)

	logger.Debugf("[%s]  registered an entry: id=%q", typeName, entryID)

	return entryID, nil
}

// RemovePeriodicTask remove periodic task.
func (s *Server) RemovePeriodicTask(typeName string) error {
	entryID := s.QueryPeriodicTaskEntryID(typeName)
	if entryID == "" {
		return errors.New(fmt.Sprintf("[%s] periodic task not exist", typeName))
	}

	if err := s.unregisterPeriodicTask(entryID); err != nil {
		logger.Errorf("[%s] dequeue periodic task failed: %s", entryID, err.Error())
		return err
	}

	s.removePeriodicTaskEntryID(typeName)

	return nil
}

func (s *Server) RemoveAllPeriodicTask() {
	s.mtxEntryIDs.Lock()
	ids := s.entryIDs
	s.entryIDs = make(map[string]string)
	s.mtxEntryIDs.Unlock()

	for _, v := range ids {
		_ = s.unregisterPeriodicTask(v)
	}
}

func (s *Server) unregisterPeriodicTask(entryID string) error {
	if s.asynqScheduler == nil {
		return nil
	}

	if err := s.asynqScheduler.Unregister(entryID); err != nil {
		logger.Errorf("[%s] dequeue periodic task failed: %s", entryID, err.Error())
		return err
	}

	return nil
}

func (s *Server) addPeriodicTaskEntryID(typeName, entryID string) {
	s.mtxEntryIDs.Lock()
	defer s.mtxEntryIDs.Unlock()

	s.entryIDs[typeName] = entryID
}

func (s *Server) removePeriodicTaskEntryID(typeName string) {
	s.mtxEntryIDs.Lock()
	defer s.mtxEntryIDs.Unlock()

	delete(s.entryIDs, typeName)
}

func (s *Server) QueryPeriodicTaskEntryID(typeName string) string {
	s.mtxEntryIDs.RLock()
	defer s.mtxEntryIDs.RUnlock()

	entryID, ok := s.entryIDs[typeName]
	if !ok {
		return ""
	}
	return entryID
}

// Start the server.
func (s *Server) Start(ctx context.Context) error {
	if s.err != nil {
		return s.err
	}

	if s.started {
		return nil
	}

	if err := s.runAsynqScheduler(); err != nil {
		logger.Error("run async scheduler failed", err)
		return err
	}

	if err := s.runAsynqServer(); err != nil {
		logger.Error("run async server failed", err)
		return err
	}

	logger.Infof("server listening on: %s", s.redisOpt.Addr)

	s.baseCtx = ctx
	s.started = true

	return nil
}

// Stop the server.
func (s *Server) Stop(_ context.Context) error {
	logger.Info("server stopping")
	s.started = false

	if s.asynqClient != nil {
		_ = s.asynqClient.Close()
		s.asynqClient = nil
	}

	if s.asynqServer != nil {
		s.asynqServer.Shutdown()
		s.asynqServer = nil
	}

	if s.asynqScheduler != nil {
		s.asynqScheduler.Shutdown()
		s.asynqScheduler = nil
	}

	if s.asynqInspector != nil {
		s.asynqInspector.Close()
		s.asynqInspector = nil
	}

	return nil
}

// createAsynqServer create asynq server.
func (s *Server) createAsynqServer() error {
	if s.asynqServer != nil {
		return nil
	}

	s.asynqServer = asynq.NewServer(s.redisOpt, s.asynqConfig)
	if s.asynqServer == nil {
		logger.Errorf("create asynq server failed")
		return errors.New("create asynq server failed")
	}
	return nil
}

// runAsynqServer run asynq server.
func (s *Server) runAsynqServer() error {
	if s.asynqServer == nil {
		logger.Errorf("asynq server is nil")
		return errors.New("asynq server is nil")
	}

	if err := s.asynqServer.Run(s.mux); err != nil {
		logger.Errorf("asynq server run failed: %s", err.Error())
		return err
	}
	return nil
}

// createAsynqClient create asynq client.
func (s *Server) createAsynqClient() error {
	if s.asynqClient != nil {
		return nil
	}

	s.asynqClient = asynq.NewClient(s.redisOpt)
	if s.asynqClient == nil {
		logger.Errorf("create asynq client failed")
		return errors.New("create asynq client failed")
	}

	return nil
}

// createAsynqScheduler create asynq scheduler.
func (s *Server) createAsynqScheduler() error {
	if s.asynqScheduler != nil {
		return nil
	}

	s.asynqScheduler = asynq.NewScheduler(s.redisOpt, s.schedulerOpts)
	if s.asynqScheduler == nil {
		logger.Errorf("create asynq scheduler failed")
		return errors.New("create asynq scheduler failed")
	}

	return nil
}

// runAsynqScheduler run asynq scheduler.
func (s *Server) runAsynqScheduler() error {
	if s.asynqScheduler == nil {
		logger.Errorf("asynq scheduler is nil")
		return errors.New("asynq scheduler is nil")
	}

	if err := s.asynqScheduler.Start(); err != nil {
		logger.Errorf("asynq scheduler start failed: %s", err.Error())
		return err
	}

	return nil
}

// createAsynqInspector create asynq inspector..
func (s *Server) createAsynqInspector() error {
	if s.asynqInspector != nil {
		return nil
	}

	s.asynqInspector = asynq.NewInspector(s.redisOpt)
	if s.asynqInspector == nil {
		logger.Errorf("create asynq inspector failed")
		return errors.New("create asynq inspector failed")
	}
	return nil
}
