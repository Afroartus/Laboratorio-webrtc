package domain_test

import (
	"testing"
	"time"

	"webrtc/api/internal/domain"
)

func TestPermissionsForRole(t *testing.T) {
	t.Parallel()

	creator := domain.PermissionsForRole(domain.RoleCreator)
	if !creator.PublishCamera || !creator.PublishMicrophone || !creator.PublishScreen || !creator.Subscribe {
		t.Fatalf("el creador debe tener todos los permisos: %+v", creator)
	}

	participant := domain.PermissionsForRole(domain.RoleParticipant)
	if !participant.PublishCamera || !participant.PublishMicrophone || !participant.PublishScreen {
		t.Fatalf("el participante debe publicar cámara, micrófono y pantalla: %+v", participant)
	}

	viewer := domain.PermissionsForRole(domain.RoleViewer)
	if viewer.PublishCamera || viewer.PublishMicrophone || viewer.PublishScreen {
		t.Fatalf("el espectador no puede publicar: %+v", viewer)
	}
	if !viewer.Subscribe {
		t.Fatalf("el espectador puede suscribirse: %+v", viewer)
	}

	unknown := domain.PermissionsForRole(domain.Role("admin"))
	if unknown.Subscribe || unknown.PublishCamera {
		t.Fatalf("un rol desconocido no tiene permisos: %+v", unknown)
	}
}

func TestAllowedSourcesForRole(t *testing.T) {
	t.Parallel()

	viewer := domain.AllowedSourcesForRole(domain.RoleViewer)
	if len(viewer) != 0 {
		t.Fatalf("el espectador no tiene fuentes: %v", viewer)
	}

	participant := domain.AllowedSourcesForRole(domain.RoleParticipant)
	if len(participant) != 3 {
		t.Fatalf("el participante tiene tres fuentes: %v", participant)
	}
}

func TestIsValidRole(t *testing.T) {
	t.Parallel()

	for _, role := range []domain.Role{domain.RoleCreator, domain.RoleParticipant, domain.RoleViewer} {
		if !domain.IsValidRole(role) {
			t.Fatalf("el rol %s debe ser válido", role)
		}
	}
	if domain.IsValidRole(domain.Role("admin")) {
		t.Fatal("un rol fuera del vocabulario no es válido")
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	t.Parallel()

	if got := domain.Backoff(0); got != 5*time.Second {
		t.Fatalf("Backoff(0) = %v, se esperaba 5s", got)
	}
	if got := domain.Backoff(1); got != 10*time.Second {
		t.Fatalf("Backoff(1) = %v, se esperaba 10s", got)
	}
	if got := domain.Backoff(2); got != 20*time.Second {
		t.Fatalf("Backoff(2) = %v, se esperaba 20s", got)
	}
	if got := domain.Backoff(-1); got != 5*time.Second {
		t.Fatalf("Backoff(-1) = %v, se esperaba 5s", got)
	}
	if got := domain.Backoff(20); got != 5*time.Minute {
		t.Fatalf("Backoff(20) = %v, se esperaba el máximo de 5m", got)
	}
}

func TestOutboxJobIsDue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	job := domain.OutboxJob{NextAttemptAt: now.Add(-time.Second)}
	if !job.IsDue(now) {
		t.Fatal("un trabajo con el próximo intento vencido está listo")
	}

	future := domain.OutboxJob{NextAttemptAt: now.Add(time.Minute)}
	if future.IsDue(now) {
		t.Fatal("un trabajo con el próximo intento en el futuro no está listo")
	}

	completed := time.Date(2026, 3, 1, 10, 1, 0, 0, time.UTC)
	done := domain.OutboxJob{NextAttemptAt: now.Add(-time.Second), CompletedAt: &completed}
	if done.IsDue(now) {
		t.Fatal("un trabajo completado no vuelve a estar listo")
	}
}
