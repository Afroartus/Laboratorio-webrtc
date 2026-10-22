package domain_test

import (
	"errors"
	"testing"
	"time"

	"webrtc/api/internal/domain"
)

func TestAllowedTransitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		from domain.CallStatus
		to   []domain.CallStatus
	}{
		{from: domain.CallCreated, to: []domain.CallStatus{domain.CallActive, domain.CallEnding, domain.CallCancelled}},
		{from: domain.CallActive, to: []domain.CallStatus{domain.CallEnding}},
		{from: domain.CallEnding, to: []domain.CallStatus{domain.CallEnded}},
		{from: domain.CallEnded, to: []domain.CallStatus{}},
		{from: domain.CallCancelled, to: []domain.CallStatus{}},
	}

	for _, tc := range cases {
		got := domain.AllowedTransitions(tc.from)
		if len(got) != len(tc.to) {
			t.Fatalf("AllowedTransitions(%s) = %v, se esperaba %v", tc.from, got, tc.to)
		}
		for _, want := range tc.to {
			found := false
			for _, status := range got {
				if status == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("AllowedTransitions(%s) = %v, falta %s", tc.from, got, want)
			}
		}
	}
}

func TestCallTransitionTo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{ID: "call-1", Status: domain.CallCreated, Version: 3}

	if err := call.TransitionTo(domain.CallActive, now); err != nil {
		t.Fatalf("CREATED → ACTIVE: %v", err)
	}
	if call.Status != domain.CallActive {
		t.Fatalf("estado = %s, se esperaba ACTIVE", call.Status)
	}
	if call.StartedAt == nil || !call.StartedAt.Equal(now) {
		t.Fatalf("StartedAt = %v, se esperaba %v", call.StartedAt, now)
	}
	if call.Version != 4 {
		t.Fatalf("version = %d, se esperaba 4", call.Version)
	}

	if err := call.TransitionTo(domain.CallEnding, now); err != nil {
		t.Fatalf("ACTIVE → ENDING: %v", err)
	}
	if err := call.TransitionTo(domain.CallEnded, now); err != nil {
		t.Fatalf("ENDING → ENDED: %v", err)
	}
	if call.EndedAt == nil || !call.EndedAt.Equal(now) {
		t.Fatalf("EndedAt = %v, se esperaba %v", call.EndedAt, now)
	}
	if !call.IsTerminal() {
		t.Fatal("una llamada terminada debe ser terminal")
	}
}

func TestCallTransitionToRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		from domain.CallStatus
		to   domain.CallStatus
	}{
		{name: "created a ended", from: domain.CallCreated, to: domain.CallEnded},
		{name: "active a created", from: domain.CallActive, to: domain.CallCreated},
		{name: "ended a active", from: domain.CallEnded, to: domain.CallActive},
		{name: "cancelled a ending", from: domain.CallCancelled, to: domain.CallEnding},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call := domain.Call{ID: "call-1", Status: tc.from, Version: 1}
			err := call.TransitionTo(tc.to, now)
			if !errors.Is(err, domain.ErrInvalidTransition) {
				t.Fatalf("error = %v, se esperaba ErrInvalidTransition", err)
			}
			if call.Status != tc.from {
				t.Fatalf("el estado cambió a %s tras una transición inválida", call.Status)
			}
			if call.Version != 1 {
				t.Fatalf("la versión cambió a %d tras una transición inválida", call.Version)
			}
		})
	}
}

func TestCallDeadlineExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	created := domain.Call{Status: domain.CallCreated, JoinDeadline: now.Add(-time.Second)}
	if !created.DeadlineExceeded(now) {
		t.Fatal("una llamada creada con el plazo vencido debe superar el plazo")
	}

	future := domain.Call{Status: domain.CallCreated, JoinDeadline: now.Add(time.Minute)}
	if future.DeadlineExceeded(now) {
		t.Fatal("una llamada creada con plazo vigente no debe superarlo")
	}

	active := domain.Call{Status: domain.CallActive, JoinDeadline: now.Add(-time.Second)}
	if active.DeadlineExceeded(now) {
		t.Fatal("solo las llamadas en CREATED tienen plazo de ingreso")
	}
}

func TestCallDurationExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	started := now.Add(-2 * time.Hour)
	active := domain.Call{Status: domain.CallActive, StartedAt: &started, MaxDurationSeconds: 3600}
	if !active.DurationExceeded(now) {
		t.Fatal("una llamada activa que superó la duración máxima debe superarla")
	}

	short := domain.Call{Status: domain.CallActive, StartedAt: &started, MaxDurationSeconds: 7200}
	if short.DurationExceeded(now) {
		t.Fatal("una llamada activa dentro de la duración máxima no debe superarla")
	}

	created := domain.Call{Status: domain.CallCreated, StartedAt: &started, MaxDurationSeconds: 60}
	if created.DurationExceeded(now) {
		t.Fatal("solo las llamadas activas tienen duración máxima")
	}
}

func TestCallMemberCanPublish(t *testing.T) {
	t.Parallel()

	revoked := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	member := domain.CallMember{
		UserID:         "user-1",
		Role:           domain.RoleParticipant,
		AllowedSources: []domain.MediaSource{domain.SourceCamera, domain.SourceMicrophone},
	}
	if !member.CanPublish(domain.SourceCamera) {
		t.Fatal("el participante puede publicar la cámara")
	}
	if member.CanPublish(domain.SourceScreen) {
		t.Fatal("el participante no puede publicar la pantalla si no está permitida")
	}

	member.RevokedAt = &revoked
	if member.CanPublish(domain.SourceCamera) {
		t.Fatal("un miembro revocado no puede publicar")
	}
}

func TestCallMemberLookup(t *testing.T) {
	t.Parallel()

	call := domain.Call{Members: []domain.CallMember{
		{UserID: "user-1", Role: domain.RoleCreator},
		{UserID: "user-2", Role: domain.RoleViewer},
	}}

	member, ok := call.Member("user-2")
	if !ok || member.Role != domain.RoleViewer {
		t.Fatalf("Member(user-2) = %v, %v", member, ok)
	}
	if _, ok := call.Member("user-3"); ok {
		t.Fatal("un usuario que no pertenece a la llamada no debe encontrarse")
	}
}
