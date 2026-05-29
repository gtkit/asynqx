package asynqx

func setProducerClientFactoryForTest(factory producerClientFactory) func() {
	previous := defaultProducerClientFactory
	defaultProducerClientFactory = factory

	return func() {
		defaultProducerClientFactory = previous
	}
}

func setWorkerRunnerFactoryForTest(factory workerRunnerFactory) func() {
	previous := defaultWorkerRunnerFactory
	defaultWorkerRunnerFactory = factory

	return func() {
		defaultWorkerRunnerFactory = previous
	}
}

func setSchedulerRunnerFactoryForTest(factory schedulerRunnerFactory) func() {
	previous := defaultSchedulerRunnerFactory
	defaultSchedulerRunnerFactory = factory

	return func() {
		defaultSchedulerRunnerFactory = previous
	}
}

func setInspectorClientFactoryForTest(factory inspectorClientFactory) func() {
	previous := defaultInspectorClientFactory
	defaultInspectorClientFactory = factory

	return func() {
		defaultInspectorClientFactory = previous
	}
}
