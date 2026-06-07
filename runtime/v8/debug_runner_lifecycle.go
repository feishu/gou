package v8

import "sync"

type debugRunnerLease struct {
	session *debugSession
	runner  *Runner
	once    sync.Once
}

func newDebugRunnerLease(session *debugSession, runner *Runner) *debugRunnerLease {
	if session == nil || runner == nil {
		return nil
	}
	return &debugRunnerLease{session: session, runner: runner}
}

func (lease *debugRunnerLease) Close() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.session != nil {
			lease.session.detachNativeForRunner(lease.runner)
		}
	})
}
