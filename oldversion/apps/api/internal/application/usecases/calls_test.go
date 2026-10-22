package usecases_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/application/usecases"
	"webrtc/api/internal/domain"
)

var testNow = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

func newTestStore() *memoryStore {
	store := newMemoryStore()
	store.users["user-1"] = domain.User{ID: "user-1", Username: "ana", DisplayName: "Ana", Active: true}
	store.users["user-2"] = domain.User{ID: "user-2", Username: "beto", DisplayName: "Beto", Active: true}
	store.users["user-3"] = domain.User{ID: "user-3", Username: "carla", DisplayName: "Carla", Active: false}
	return store
}

// idSeq genera identificadores legibles y únicos en cada prueba.
func idSeq(prefix string) func() string {
	counter := 0
	return func() string {
		counter++
		return fmt.Sprintf("%s-%d", prefix, counter)
	}
}

func TestCreateCall(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	uow := &fakeUnitOfWork{store: store}
	clock := &fakeClock{now: testNow}
	useCase := usecases.NewCreateCall(uow, clock, idSeq("call"), time.Hour, 10*time.Minute)

	result, err := useCase.Execute(context.Background(), ports.CreateCallInput{
		ActorID:   "user-1",
		InviteeID: "user-2",
	})
	if err != nil {
		t.Fatalf("CreateCall: %v", err)
	}
	if result.Status != domain.CallCreated {
		t.Fatalf("estado = %s, se esperaba CREATED", result.Status)
	}

	call, ok := store.calls[result.CallID]
	if !ok {
		t.Fatalf("no se guardó la llamada %s", result.CallID)
	}
	if call.OwnerID != "user-1" {
		t.Fatalf("creador = %s, se esperaba user-1", call.OwnerID)
	}
	if call.RoomName != "call_"+string(result.CallID) {
		t.Fatalf("sala = %s", call.RoomName)
	}
	if !call.JoinDeadline.Equal(testNow.Add(10 * time.Minute)) {
		t.Fatalf("plazo de ingreso = %v", call.JoinDeadline)
	}
	if call.MaxDurationSeconds != 3600 {
		t.Fatalf("duración máxima = %d", call.MaxDurationSeconds)
	}

	members := store.members[result.CallID]
	if len(members) != 2 {
		t.Fatalf("miembros = %d, se esperaban 2", len(members))
	}
	if members[0].Role != domain.RoleCreator || members[1].Role != domain.RoleParticipant {
		t.Fatalf("roles = %s, %s", members[0].Role, members[1].Role)
	}
	if len(members[1].AllowedSources) != 3 {
		t.Fatalf("fuentes del invitado = %v", members[1].AllowedSources)
	}
	if len(store.events) != 1 || store.events[0].EventType != "call.created" {
		t.Fatalf("eventos = %v", store.events)
	}
}

func TestCreateCallRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   ports.CreateCallInput
	}{
		{name: "mismo actor e invitado", in: ports.CreateCallInput{ActorID: "user-1", InviteeID: "user-1"}},
		{name: "actor vacío", in: ports.CreateCallInput{ActorID: "", InviteeID: "user-2"}},
		{name: "invitado inexistente", in: ports.CreateCallInput{ActorID: "user-1", InviteeID: "user-9"}},
		{name: "invitado inactivo", in: ports.CreateCallInput{ActorID: "user-1", InviteeID: "user-3"}},
		{name: "rol creador", in: ports.CreateCallInput{ActorID: "user-1", InviteeID: "user-2", InviteeRole: domain.RoleCreator}},
		{name: "rol inválido", in: ports.CreateCallInput{ActorID: "user-1", InviteeID: "user-2", InviteeRole: domain.Role("admin")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newTestStore()
			uow := &fakeUnitOfWork{store: store}
			useCase := usecases.NewCreateCall(uow, &fakeClock{now: testNow}, idSeq("call"), time.Hour, 10*time.Minute)

			if _, err := useCase.Execute(context.Background(), tc.in); !errors.Is(err, ports.ErrInvalidInput) {
				t.Fatalf("error = %v, se esperaba ErrInvalidInput", err)
			}
			if len(store.calls) != 0 {
				t.Fatalf("no debe quedar ninguna llamada guardada: %v", store.calls)
			}
		})
	}
}

func TestCreateCallRollsBackOnFailure(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	uow := &fakeUnitOfWork{store: store, failOn: errors.New("fallo de base de datos")}
	useCase := usecases.NewCreateCall(uow, &fakeClock{now: testNow}, idSeq("call"), time.Hour, 10*time.Minute)

	if _, err := useCase.Execute(context.Background(), ports.CreateCallInput{ActorID: "user-1", InviteeID: "user-2"}); err == nil {
		t.Fatal("se esperaba un error")
	}
	if len(store.calls) != 0 || len(store.members) != 0 || len(store.events) != 0 {
		t.Fatal("la transacción fallida no debe dejar datos")
	}
}

func TestEndCallMovesCallToEndingAndEnqueuesJob(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{
		ID:                 "call-1",
		RoomName:           "call_call-1",
		OwnerID:            "user-1",
		Status:             domain.CallActive,
		CreatedAt:          testNow,
		MaxDurationSeconds: 3600,
		Version:            1,
	}
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewEndCall(uow, &fakeClock{now: testNow}, idSeq("job"))

	result, err := useCase.Execute(context.Background(), ports.CallAccess{ActorID: "user-1", CallID: "call-1"})
	if err != nil {
		t.Fatalf("EndCall: %v", err)
	}
	if result.Status != domain.CallEnding {
		t.Fatalf("estado = %s, se esperaba ENDING", result.Status)
	}

	call := store.calls["call-1"]
	if call.Status != domain.CallEnding || call.Version != 2 {
		t.Fatalf("llamada = %+v", call)
	}
	if len(store.outbox) != 1 {
		t.Fatalf("trabajos = %d, se esperaba 1", len(store.outbox))
	}
	job := store.outbox[0]
	if job.Operation != domain.OperationDeleteRoom || job.CallID != "call-1" {
		t.Fatalf("trabajo = %+v", job)
	}
	if len(store.events) != 1 || store.events[0].EventType != "call.ending" {
		t.Fatalf("eventos = %v", store.events)
	}
}

func TestEndCallRejectsNonOwner(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{ID: "call-1", OwnerID: "user-1", Status: domain.CallActive, Version: 1}
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewEndCall(uow, &fakeClock{now: testNow}, idSeq("job"))

	_, err := useCase.Execute(context.Background(), ports.CallAccess{ActorID: "user-2", CallID: "call-1"})
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("error = %v, se esperaba ErrForbidden", err)
	}
	if store.calls["call-1"].Status != domain.CallActive {
		t.Fatal("la llamada no debe cambiar de estado")
	}
}

func TestEndCallIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{ID: "call-1", OwnerID: "user-1", Status: domain.CallEnding, Version: 3}
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewEndCall(uow, &fakeClock{now: testNow}, idSeq("job"))

	result, err := useCase.Execute(context.Background(), ports.CallAccess{ActorID: "user-1", CallID: "call-1"})
	if err != nil {
		t.Fatalf("EndCall: %v", err)
	}
	if result.Status != domain.CallEnding {
		t.Fatalf("estado = %s, se esperaba ENDING", result.Status)
	}
	if len(store.outbox) != 0 {
		t.Fatalf("no debe encolarse otro trabajo: %v", store.outbox)
	}
	if len(store.events) != 0 {
		t.Fatalf("no debe registrarse otro evento: %v", store.events)
	}
}

func TestEndCallRejectsUnknownCall(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewEndCall(uow, &fakeClock{now: testNow}, idSeq("job"))

	_, err := useCase.Execute(context.Background(), ports.CallAccess{ActorID: "user-1", CallID: "call-9"})
	if !errors.Is(err, ports.ErrCallNotFound) {
		t.Fatalf("error = %v, se esperaba ErrCallNotFound", err)
	}
}

func TestActivateCall(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{ID: "call-1", OwnerID: "user-1", Status: domain.CallCreated, Version: 1}
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewActivateCall(uow, &fakeClock{now: testNow}, idSeq("session"))

	err := useCase.Execute(context.Background(), usecases.ActivateCallInput{
		CallID:         "call-1",
		UserID:         "user-1",
		ParticipantSID: "PA_1",
		RoomSID:        "RM_1",
	})
	if err != nil {
		t.Fatalf("ActivateCall: %v", err)
	}

	call := store.calls["call-1"]
	if call.Status != domain.CallActive {
		t.Fatalf("estado = %s, se esperaba ACTIVE", call.Status)
	}
	if call.StartedAt == nil || !call.StartedAt.Equal(testNow) {
		t.Fatalf("inicio = %v", call.StartedAt)
	}
	if len(store.sessions) != 1 {
		t.Fatalf("sesiones = %d, se esperaba 1", len(store.sessions))
	}
	if store.sessions[0].ParticipantSID != "PA_1" {
		t.Fatalf("sesión = %+v", store.sessions[0])
	}
	if len(store.events) != 1 || store.events[0].EventType != "call.active" {
		t.Fatalf("eventos = %v", store.events)
	}
}

func TestActivateCallRecordsSessionWithoutChangingState(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{ID: "call-1", OwnerID: "user-1", Status: domain.CallActive, Version: 2}
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewActivateCall(uow, &fakeClock{now: testNow}, idSeq("session"))

	err := useCase.Execute(context.Background(), usecases.ActivateCallInput{
		CallID:         "call-1",
		UserID:         "user-2",
		ParticipantSID: "PA_2",
		RoomSID:        "RM_1",
	})
	if err != nil {
		t.Fatalf("ActivateCall: %v", err)
	}
	if store.calls["call-1"].Status != domain.CallActive {
		t.Fatal("una llamada ya activa no cambia de estado")
	}
	if len(store.sessions) != 1 {
		t.Fatalf("sesiones = %d, se esperaba 1", len(store.sessions))
	}
	if len(store.events) != 0 {
		t.Fatalf("no debe registrarse un evento nuevo: %v", store.events)
	}
}

func TestActivateCallRejectsTerminatedCall(t *testing.T) {
	t.Parallel()

	store := newTestStore()
	store.calls["call-1"] = domain.Call{ID: "call-1", OwnerID: "user-1", Status: domain.CallEnded, Version: 4}
	uow := &fakeUnitOfWork{store: store}
	useCase := usecases.NewActivateCall(uow, &fakeClock{now: testNow}, idSeq("session"))

	err := useCase.Execute(context.Background(), usecases.ActivateCallInput{CallID: "call-1", UserID: "user-1"})
	if !errors.Is(err, ports.ErrCallEnded) {
		t.Fatalf("error = %v, se esperaba ErrCallEnded", err)
	}
	if len(store.sessions) != 0 {
		t.Fatal("no debe registrarse ninguna sesión")
	}
}
