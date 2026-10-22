package httpadapter

import (
	"context"
	"sync"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// Dobles de los puertos de entrada. Registran las invocaciones para comprobar
// que el adaptador no llega al núcleo cuando una validación previa falla.

type fakeAuth struct {
	mu           sync.Mutex
	sessions     map[string]ports.Session
	login        func(ports.LoginInput) (ports.LoginResult, error)
	logoutErr    error
	loginCalls   int
	logoutTokens []string
}

func (f *fakeAuth) Login(_ context.Context, in ports.LoginInput) (ports.LoginResult, error) {
	f.mu.Lock()
	f.loginCalls++
	f.mu.Unlock()
	return f.login(in)
}

func (f *fakeAuth) Logout(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logoutTokens = append(f.logoutTokens, token)
	return f.logoutErr
}

func (f *fakeAuth) ResolveSession(_ context.Context, token string) (ports.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[token]
	if !ok {
		return ports.Session{}, ports.ErrUnauthenticated
	}
	return s, nil
}

func (f *fakeAuth) LoginCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCalls
}

type fakeUsers struct {
	users  []ports.UserSummary
	err    error
	actors []domain.UserID
}

func (f *fakeUsers) ListActiveUsers(_ context.Context, actor domain.UserID) ([]ports.UserSummary, error) {
	f.actors = append(f.actors, actor)
	return f.users, f.err
}

type fakeCalls struct {
	mu        sync.Mutex
	create    func(ports.CreateCallInput) (ports.CreateCallResult, error)
	get       func(ports.CallAccess) (ports.CallView, error)
	joinToken func(ports.CallAccess) (ports.JoinCredentials, error)
	end       func(ports.CallAccess) (ports.EndCallResult, error)
	invoked   int
}

func (f *fakeCalls) record() {
	f.mu.Lock()
	f.invoked++
	f.mu.Unlock()
}

func (f *fakeCalls) Invoked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.invoked
}

func (f *fakeCalls) CreateCall(_ context.Context, in ports.CreateCallInput) (ports.CreateCallResult, error) {
	f.record()
	return f.create(in)
}

func (f *fakeCalls) GetCall(_ context.Context, in ports.CallAccess) (ports.CallView, error) {
	f.record()
	return f.get(in)
}

func (f *fakeCalls) IssueJoinToken(_ context.Context, in ports.CallAccess) (ports.JoinCredentials, error) {
	f.record()
	return f.joinToken(in)
}

func (f *fakeCalls) EndCall(_ context.Context, in ports.CallAccess) (ports.EndCallResult, error) {
	f.record()
	return f.end(in)
}

type fakeWebhooks struct {
	err           error
	body          []byte
	authorization string
}

func (f *fakeWebhooks) Receive(_ context.Context, body []byte, authorization string) error {
	f.body = body
	f.authorization = authorization
	return f.err
}
