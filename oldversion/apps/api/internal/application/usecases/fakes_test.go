package usecases_test

import (
	"context"
	"errors"
	"sort"
	"time"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// ---------------------------------------------------------------------------
// Reloj falso
// ---------------------------------------------------------------------------

// fakeClock devuelve siempre el mismo instante.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

// ---------------------------------------------------------------------------
// Almacén en memoria con copia para simular la reversión de una transacción
// ---------------------------------------------------------------------------

type memoryStore struct {
	calls    map[domain.CallID]domain.Call
	members  map[domain.CallID][]domain.CallMember
	sessions []domain.CallSession
	events   []domain.CallEvent
	outbox   []domain.OutboxJob
	users    map[domain.UserID]domain.User
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		calls:   map[domain.CallID]domain.Call{},
		members: map[domain.CallID][]domain.CallMember{},
		users:   map[domain.UserID]domain.User{},
	}
}

func (s *memoryStore) clone() *memoryStore {
	copied := newMemoryStore()
	for id, call := range s.calls {
		copied.calls[id] = call
	}
	for id, members := range s.members {
		copied.members[id] = append([]domain.CallMember(nil), members...)
	}
	copied.sessions = append([]domain.CallSession(nil), s.sessions...)
	copied.events = append([]domain.CallEvent(nil), s.events...)
	copied.outbox = append([]domain.OutboxJob(nil), s.outbox...)
	for id, user := range s.users {
		copied.users[id] = user
	}
	return copied
}

// fakeUnitOfWork ejecuta la función con un repositorio en memoria. Si la
// función devuelve error, los cambios se revierten.
type fakeUnitOfWork struct {
	store *memoryStore
	// failOn obliga a fallar la transacción con este error.
	failOn error
	// transactions cuenta las transacciones abiertas.
	transactions int
}

func (u *fakeUnitOfWork) WithinTransaction(ctx context.Context, fn func(context.Context, ports.Repositories) error) error {
	if u.failOn != nil {
		return u.failOn
	}
	u.transactions++
	snapshot := u.store.clone()
	repos := &fakeRepos{store: u.store}
	if err := fn(ctx, repos); err != nil {
		u.store.calls = snapshot.calls
		u.store.members = snapshot.members
		u.store.sessions = snapshot.sessions
		u.store.events = snapshot.events
		u.store.outbox = snapshot.outbox
		u.store.users = snapshot.users
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Repositorios en memoria
// ---------------------------------------------------------------------------

type fakeRepos struct {
	store *memoryStore
}

func (r *fakeRepos) Calls() ports.CallRepository         { return &fakeCallRepo{store: r.store} }
func (r *fakeRepos) Members() ports.CallMemberRepository { return &fakeMemberRepo{store: r.store} }
func (r *fakeRepos) Sessions() ports.CallSessionRepository {
	return &fakeSessionRepo{store: r.store}
}
func (r *fakeRepos) Events() ports.EventRepository  { return &fakeEventRepo{store: r.store} }
func (r *fakeRepos) Outbox() ports.OutboxRepository { return &fakeOutboxRepo{store: r.store} }
func (r *fakeRepos) Users() ports.UserRepository    { return &fakeUserRepo{store: r.store} }

type fakeCallRepo struct {
	store *memoryStore
}

func (r *fakeCallRepo) Insert(_ context.Context, call domain.Call) error {
	if _, exists := r.store.calls[call.ID]; exists {
		return errors.New("llamada duplicada")
	}
	r.store.calls[call.ID] = call
	return nil
}

func (r *fakeCallRepo) GetByID(_ context.Context, id domain.CallID) (domain.Call, error) {
	call, ok := r.store.calls[id]
	if !ok {
		return domain.Call{}, ports.ErrCallNotFound
	}
	return call, nil
}

func (r *fakeCallRepo) GetByIDForUpdate(ctx context.Context, id domain.CallID) (domain.Call, error) {
	return r.GetByID(ctx, id)
}

func (r *fakeCallRepo) UpdateStatus(_ context.Context, id domain.CallID, expectedVersion int, from, to domain.CallStatus, now time.Time) (int, error) {
	call, ok := r.store.calls[id]
	if !ok {
		return 0, ports.ErrCallNotFound
	}
	if call.Version != expectedVersion || call.Status != from {
		return 0, ports.ErrCallStateConflict
	}
	call.Status = to
	call.Version++
	if to == domain.CallActive && call.StartedAt == nil {
		started := now
		call.StartedAt = &started
	}
	if to == domain.CallEnded || to == domain.CallCancelled {
		end := now
		call.EndedAt = &end
	}
	r.store.calls[id] = call
	return call.Version, nil
}

func (r *fakeCallRepo) ListByStatus(_ context.Context, status domain.CallStatus, limit int) ([]domain.Call, error) {
	var out []domain.Call
	for _, call := range r.store.calls {
		if call.Status == status {
			out = append(out, call)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeCallRepo) ListJoinDeadlineExceeded(_ context.Context, now time.Time, limit int) ([]domain.Call, error) {
	var out []domain.Call
	for _, call := range r.store.calls {
		if call.Status == domain.CallCreated && now.After(call.JoinDeadline) {
			out = append(out, call)
		}
	}
	return out, nil
}

func (r *fakeCallRepo) ListDurationExceeded(_ context.Context, now time.Time, limit int) ([]domain.Call, error) {
	var out []domain.Call
	for _, call := range r.store.calls {
		if call.Status != domain.CallActive || call.StartedAt == nil {
			continue
		}
		limit := call.StartedAt.Add(time.Duration(call.MaxDurationSeconds) * time.Second)
		if now.After(limit) {
			out = append(out, call)
		}
	}
	return out, nil
}

type fakeMemberRepo struct {
	store *memoryStore
}

func (r *fakeMemberRepo) InsertMembers(_ context.Context, callID domain.CallID, members []domain.CallMember) error {
	r.store.members[callID] = append(r.store.members[callID], members...)
	return nil
}

func (r *fakeMemberRepo) ListByCall(_ context.Context, callID domain.CallID) ([]domain.CallMember, error) {
	return append([]domain.CallMember(nil), r.store.members[callID]...), nil
}

func (r *fakeMemberRepo) Get(_ context.Context, callID domain.CallID, userID domain.UserID) (domain.CallMember, error) {
	for _, member := range r.store.members[callID] {
		if member.UserID == userID {
			return member, nil
		}
	}
	return domain.CallMember{}, ports.ErrCallNotFound
}

type fakeSessionRepo struct {
	store *memoryStore
}

func (r *fakeSessionRepo) Insert(_ context.Context, session domain.CallSession) error {
	r.store.sessions = append(r.store.sessions, session)
	return nil
}

func (r *fakeSessionRepo) Close(_ context.Context, id string, at time.Time, reason string) error {
	for i := range r.store.sessions {
		if r.store.sessions[i].ID == id {
			left := at
			r.store.sessions[i].LeftAt = &left
			r.store.sessions[i].DisconnectReason = reason
			return nil
		}
	}
	return ports.ErrCallNotFound
}

func (r *fakeSessionRepo) ListOpenByCall(_ context.Context, callID domain.CallID) ([]domain.CallSession, error) {
	var out []domain.CallSession
	for _, session := range r.store.sessions {
		if session.CallID == callID && session.LeftAt == nil {
			out = append(out, session)
		}
	}
	return out, nil
}

type fakeEventRepo struct {
	store *memoryStore
}

func (r *fakeEventRepo) Append(_ context.Context, event domain.CallEvent) error {
	r.store.events = append(r.store.events, event)
	return nil
}

func (r *fakeEventRepo) AppendProviderEvent(_ context.Context, event domain.CallEvent) (bool, error) {
	for _, existing := range r.store.events {
		if existing.ProviderEventID != "" && existing.ProviderEventID == event.ProviderEventID {
			return false, nil
		}
	}
	r.store.events = append(r.store.events, event)
	return true, nil
}

type fakeOutboxRepo struct {
	store *memoryStore
}

func (r *fakeOutboxRepo) Enqueue(_ context.Context, job domain.OutboxJob) error {
	for _, existing := range r.store.outbox {
		if existing.CallID == job.CallID && existing.Operation == job.Operation && existing.CompletedAt == nil {
			return nil
		}
	}
	r.store.outbox = append(r.store.outbox, job)
	return nil
}

func (r *fakeOutboxRepo) ClaimDue(_ context.Context, now time.Time, limit int, lease time.Duration) ([]domain.OutboxJob, error) {
	var claimed []domain.OutboxJob
	for i := range r.store.outbox {
		job := r.store.outbox[i]
		if job.CompletedAt != nil || job.NextAttemptAt.After(now) {
			continue
		}
		job.NextAttemptAt = now.Add(lease)
		r.store.outbox[i] = job
		claimed = append(claimed, job)
		if limit > 0 && len(claimed) == limit {
			break
		}
	}
	return claimed, nil
}

func (r *fakeOutboxRepo) MarkCompleted(_ context.Context, id string, now time.Time) error {
	for i := range r.store.outbox {
		if r.store.outbox[i].ID == id {
			completed := now
			r.store.outbox[i].CompletedAt = &completed
			return nil
		}
	}
	return errors.New("trabajo inexistente")
}

func (r *fakeOutboxRepo) MarkFailed(_ context.Context, id string, attempts int, nextAttemptAt time.Time, lastError string) error {
	for i := range r.store.outbox {
		if r.store.outbox[i].ID == id {
			r.store.outbox[i].Attempts = attempts
			r.store.outbox[i].NextAttemptAt = nextAttemptAt
			r.store.outbox[i].LastError = lastError
			return nil
		}
	}
	return errors.New("trabajo inexistente")
}

func (r *fakeOutboxRepo) MarkAbandoned(_ context.Context, id string, attempts int, now time.Time, lastError string) error {
	for i := range r.store.outbox {
		if r.store.outbox[i].ID == id {
			completed := now
			r.store.outbox[i].Attempts = attempts
			r.store.outbox[i].LastError = lastError
			r.store.outbox[i].CompletedAt = &completed
			return nil
		}
	}
	return errors.New("trabajo inexistente")
}

type fakeUserRepo struct {
	store *memoryStore
}

func (r *fakeUserRepo) GetByID(_ context.Context, id domain.UserID) (domain.User, error) {
	user, ok := r.store.users[id]
	if !ok {
		return domain.User{}, ports.ErrInvalidInput
	}
	return user, nil
}

func (r *fakeUserRepo) GetByUsername(_ context.Context, username string) (domain.User, error) {
	for _, user := range r.store.users {
		if user.Username == username {
			return user, nil
		}
	}
	return domain.User{}, ports.ErrInvalidInput
}

// ---------------------------------------------------------------------------
// Servidor de medios falso
// ---------------------------------------------------------------------------

// fakeRoomService simula las salas del servidor de medios.
type fakeRoomService struct {
	rooms map[string]ports.RoomInfo
	// deleteErr obliga a fallar la eliminación de la sala.
	deleteErr error
	// deleted guarda el orden de las eliminaciones.
	deleted []string
}

func newFakeRoomService() *fakeRoomService {
	return &fakeRoomService{rooms: map[string]ports.RoomInfo{}}
}

func (s *fakeRoomService) GetRoom(_ context.Context, roomName string) (ports.RoomInfo, error) {
	room, ok := s.rooms[roomName]
	if !ok {
		return ports.RoomInfo{}, ports.ErrRoomNotFound
	}
	return room, nil
}

func (s *fakeRoomService) DeleteRoom(_ context.Context, roomName string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.rooms, roomName)
	s.deleted = append(s.deleted, roomName)
	return nil
}
