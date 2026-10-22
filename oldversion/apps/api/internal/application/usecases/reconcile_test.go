package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/application/usecases"
	"webrtc/api/internal/domain"
)

func TestReconcileCancelsCallsWithoutParticipants(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallCreated,
		JoinDeadline:       testNow.Add(-time.Minute),
		MaxDurationSeconds: 3600,
		Version:            1,
	}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	useCase := usecases.NewReconcileCalls(uow, &fakeClock{now: testNow}, rooms, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ReconcileCalls: %v", err)
	}
	if result.Cancelled != 1 {
		t.Fatalf("canceladas = %d, se esperaba 1", result.Cancelled)
	}
	call := store.calls["call-1"]
	if call.Status != domain.CallCancelled {
		t.Fatalf("estado = %s, se esperaba CANCELLED", call.Status)
	}
	if call.EndedAt == nil {
		t.Fatal("una llamada cancelada registra su fin")
	}
	if len(store.events) != 1 || store.events[0].EventType != "call.cancelled" {
		t.Fatalf("eventos = %v", store.events)
	}
}

func TestReconcileMovesOverdueCallsToEnding(t *testing.T) {
	t.Parallel()

	started := testNow.Add(-2 * time.Hour)
	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallActive,
		StartedAt:          &started,
		MaxDurationSeconds: 3600,
		Version:            3,
	}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	rooms.rooms["call_call-1"] = ports.RoomInfo{Name: "call_call-1"}
	useCase := usecases.NewReconcileCalls(uow, &fakeClock{now: testNow}, rooms, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ReconcileCalls: %v", err)
	}
	if result.Ending != 1 {
		t.Fatalf("en cierre = %d, se esperaba 1", result.Ending)
	}
	if store.calls["call-1"].Status != domain.CallEnding {
		t.Fatalf("estado = %s, se esperaba ENDING", store.calls["call-1"].Status)
	}
	if len(store.outbox) != 1 || store.outbox[0].Operation != domain.OperationDeleteRoom {
		t.Fatalf("trabajos = %v", store.outbox)
	}
}

func TestReconcileEndsCallsWhoseRoomIsGone(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallEnding,
		MaxDurationSeconds: 3600,
		Version:            5,
	}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	useCase := usecases.NewReconcileCalls(uow, &fakeClock{now: testNow}, rooms, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ReconcileCalls: %v", err)
	}
	if result.Ended != 1 {
		t.Fatalf("terminadas = %d, se esperaba 1", result.Ended)
	}
	if store.calls["call-1"].Status != domain.CallEnded {
		t.Fatalf("estado = %s, se esperaba ENDED", store.calls["call-1"].Status)
	}
}

func TestReconcileReportsOrphansWithoutChangingState(t *testing.T) {
	t.Parallel()

	started := testNow.Add(-time.Minute)
	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallActive,
		StartedAt:          &started,
		MaxDurationSeconds: 3600,
		Version:            2,
	}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	useCase := usecases.NewReconcileCalls(uow, &fakeClock{now: testNow}, rooms, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ReconcileCalls: %v", err)
	}
	if result.Orphans != 1 {
		t.Fatalf("huérfanas = %d, se esperaba 1", result.Orphans)
	}
	if store.calls["call-1"].Status != domain.CallActive {
		t.Fatal("una llamada huérfana no cambia de estado")
	}
}

func TestProcessOutboxDeletesRoomAndEndsCall(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallEnding,
		MaxDurationSeconds: 3600,
		Version:            6,
	}
	store.outbox = []domain.OutboxJob{{
		ID:            "job-1",
		CallID:        "call-1",
		Operation:     domain.OperationDeleteRoom,
		NextAttemptAt: testNow,
		CreatedAt:     testNow,
	}}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	rooms.rooms["call_call-1"] = ports.RoomInfo{Name: "call_call-1"}
	useCase := usecases.NewProcessOutbox(uow, &fakeClock{now: testNow}, rooms, 3, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ProcessOutbox: %v", err)
	}
	if result.Completed != 1 {
		t.Fatalf("completados = %d, se esperaba 1", result.Completed)
	}
	if len(rooms.deleted) != 1 || rooms.deleted[0] != "call_call-1" {
		t.Fatalf("salas eliminadas = %v", rooms.deleted)
	}
	if store.outbox[0].CompletedAt == nil {
		t.Fatal("el trabajo debe quedar completado")
	}
	if store.calls["call-1"].Status != domain.CallEnded {
		t.Fatalf("estado = %s, se esperaba ENDED", store.calls["call-1"].Status)
	}
	if len(store.events) != 1 || store.events[0].EventType != "call.ended" {
		t.Fatalf("eventos = %v", store.events)
	}
}

func TestProcessOutboxSchedulesRetryOnFailure(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallEnding,
		MaxDurationSeconds: 3600,
		Version:            6,
	}
	store.outbox = []domain.OutboxJob{{
		ID:            "job-1",
		CallID:        "call-1",
		Operation:     domain.OperationDeleteRoom,
		NextAttemptAt: testNow,
		CreatedAt:     testNow,
	}}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	rooms.deleteErr = errors.New("el servidor de medios no responde")
	useCase := usecases.NewProcessOutbox(uow, &fakeClock{now: testNow}, rooms, 3, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ProcessOutbox: %v", err)
	}
	if result.Failed != 1 {
		t.Fatalf("fallidos = %d, se esperaba 1", result.Failed)
	}
	job := store.outbox[0]
	if job.Attempts != 1 {
		t.Fatalf("intentos = %d, se esperaba 1", job.Attempts)
	}
	if !job.NextAttemptAt.Equal(testNow.Add(domain.Backoff(1))) {
		t.Fatalf("próximo intento = %v", job.NextAttemptAt)
	}
	if job.LastError == "" {
		t.Fatal("el trabajo debe guardar el error")
	}
	if store.calls["call-1"].Status != domain.CallEnding {
		t.Fatal("la llamada sigue en ENDING hasta que el trabajo termine")
	}
}

func TestProcessOutboxAbandonsJobAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallEnding,
		MaxDurationSeconds: 3600,
		Version:            6,
	}
	store.outbox = []domain.OutboxJob{{
		ID:            "job-1",
		CallID:        "call-1",
		Operation:     domain.OperationDeleteRoom,
		Attempts:      2,
		NextAttemptAt: testNow,
		CreatedAt:     testNow,
	}}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	rooms.deleteErr = errors.New("el servidor de medios no responde")
	useCase := usecases.NewProcessOutbox(uow, &fakeClock{now: testNow}, rooms, 3, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ProcessOutbox: %v", err)
	}
	if result.Abandoned != 1 {
		t.Fatalf("abandonados = %d, se esperaba 1", result.Abandoned)
	}
	job := store.outbox[0]
	if job.CompletedAt == nil {
		t.Fatal("un trabajo abandonado queda cerrado")
	}
	if job.Attempts != 3 {
		t.Fatalf("intentos = %d, se esperaban 3", job.Attempts)
	}
}

func TestProcessOutboxIgnoresJobsNotDue(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.outbox = []domain.OutboxJob{{
		ID:            "job-1",
		CallID:        "call-1",
		Operation:     domain.OperationDeleteRoom,
		NextAttemptAt: testNow.Add(time.Minute),
		CreatedAt:     testNow,
	}}
	uow := &fakeUnitOfWork{store: store}
	rooms := newFakeRoomService()
	useCase := usecases.NewProcessOutbox(uow, &fakeClock{now: testNow}, rooms, 3, idSeq("event"))

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("ProcessOutbox: %v", err)
	}
	if result.Completed != 0 || result.Failed != 0 {
		t.Fatalf("resultado = %+v", result)
	}
	if len(rooms.deleted) != 0 {
		t.Fatalf("no se debe eliminar ninguna sala: %v", rooms.deleted)
	}
}
