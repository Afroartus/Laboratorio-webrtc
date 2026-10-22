//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"webrtc/api/internal/adapters/out/postgres"
	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// dsn de las pruebas de integración. Si no hay ninguna configurada, las
// pruebas se omiten en lugar de fallar.
func testDSN(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if dsn := strings.TrimSpace(os.Getenv(name)); dsn != "" {
			return dsn
		}
	}
	t.Skip("no hay TEST_DATABASE_URL ni DATABASE_URL: se omiten las pruebas de integración")
	return ""
}

// dsnWithSchema devuelve la misma URL con el esquema de pruebas como
// search_path de cada conexión.
func dsnWithSchema(dsn, schema string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "options=" + url.QueryEscape("-c search_path="+schema)
}

// testDB agrupa el pool del adaptador y el pool de administración con el
// nombre del esquema de pruebas.
type testDB struct {
	adapter *postgres.Pool
	admin   *pgxpool.Pool
	schema  string
}

// newTestDatabase crea un esquema aislado con el esquema de la migración y
// devuelve el pool del adaptador apuntando a él. Al terminar la prueba se
// borra el esquema.
func newTestDatabase(t *testing.T) testDB {
	t.Helper()
	ctx := context.Background()
	dsn := testDSN(t)

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("no se pudo conectar a PostgreSQL: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("no se pudo crear el esquema de pruebas: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	applyMigration(t, ctx, admin, schema)

	pool, err := postgres.Open(ctx, dsnWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("no se pudo crear el pool de pruebas: %v", err)
	}
	t.Cleanup(pool.Close)
	return testDB{adapter: pool, admin: admin, schema: schema}
}

// applyMigration ejecuta la migración inicial dentro del esquema de pruebas.
func applyMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "migrations", "000001_init.up.sql")
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no se pudo leer la migración %s: %v", path, err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("no se pudo obtener una conexión: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("no se pudo fijar el esquema de pruebas: %v", err)
	}
	if _, err := conn.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("no se pudo aplicar la migración: %v", err)
	}
}

// insertUser crea un usuario de pruebas y devuelve su identificador.
func insertUser(t *testing.T, ctx context.Context, db testDB, username string) domain.UserID {
	t.Helper()
	var id string
	err := db.admin.QueryRow(ctx, `
		INSERT INTO `+db.schema+`.users (username, display_name, password_hash, active)
		VALUES ($1, $2, 'hash', true)
		RETURNING id::text`, username, username).Scan(&id)
	if err != nil {
		t.Fatalf("no se pudo crear el usuario de pruebas: %v", err)
	}
	return domain.UserID(id)
}

func TestUserRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	repos := postgres.NewStore(db.adapter).Repositories()

	id := insertUser(t, ctx, db, "ana")
	user, err := repos.Users().GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if user.Username != "ana" || !user.Active {
		t.Fatalf("usuario = %+v", user)
	}

	byName, err := repos.Users().GetByUsername(ctx, "ana")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if byName.ID != id {
		t.Fatalf("id = %s, se esperaba %s", byName.ID, id)
	}

	if _, err := repos.Users().GetByUsername(ctx, "no-existe"); !errors.Is(err, ports.ErrInvalidInput) {
		t.Fatalf("error = %v, se esperaba ErrInvalidInput", err)
	}
}

func TestCallRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	store := postgres.NewStore(db.adapter)
	repos := store.Repositories()

	owner := insertUser(t, ctx, db, "ana")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_test",
		OwnerID:            owner,
		Status:             domain.CallCreated,
		CreatedAt:          now,
		JoinDeadline:       now.Add(10 * time.Minute),
		MaxDurationSeconds: 3600,
		Version:            0,
	}
	if err := repos.Calls().Insert(ctx, call); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	saved, err := repos.Calls().GetByID(ctx, call.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if saved.Status != domain.CallCreated || saved.OwnerID != owner {
		t.Fatalf("llamada = %+v", saved)
	}

	version, err := repos.Calls().UpdateStatus(ctx, call.ID, 0, domain.CallCreated, domain.CallActive, now)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if version != 1 {
		t.Fatalf("versión = %d, se esperaba 1", version)
	}

	active, err := repos.Calls().GetByID(ctx, call.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if active.StartedAt == nil || !active.StartedAt.Equal(now) {
		t.Fatalf("inicio = %v, se esperaba %v", active.StartedAt, now)
	}

	// La versión esperada evita que dos transacciones se pisen.
	if _, err := repos.Calls().UpdateStatus(ctx, call.ID, 0, domain.CallCreated, domain.CallEnding, now); !errors.Is(err, ports.ErrCallStateConflict) {
		t.Fatalf("error = %v, se esperaba ErrCallStateConflict", err)
	}
}

func TestCallRepositoryLists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	repos := postgres.NewStore(db.adapter).Repositories()

	owner := insertUser(t, ctx, db, "ana")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	started := now.Add(-2 * time.Hour)

	expired := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_expirada",
		OwnerID:            owner,
		Status:             domain.CallCreated,
		CreatedAt:          now,
		JoinDeadline:       now.Add(-time.Minute),
		MaxDurationSeconds: 3600,
	}
	if err := repos.Calls().Insert(ctx, expired); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	overdue := domain.Call{
		ID:                 domain.CallID("22222222-2222-4222-8222-222222222222"),
		RoomName:           "call_vencida",
		OwnerID:            owner,
		Status:             domain.CallActive,
		CreatedAt:          now,
		StartedAt:          &started,
		MaxDurationSeconds: 3600,
	}
	if err := repos.Calls().Insert(ctx, overdue); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	expiredList, err := repos.Calls().ListJoinDeadlineExceeded(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListJoinDeadlineExceeded: %v", err)
	}
	if len(expiredList) != 1 || expiredList[0].ID != expired.ID {
		t.Fatalf("caducadas = %+v", expiredList)
	}

	durationList, err := repos.Calls().ListDurationExceeded(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDurationExceeded: %v", err)
	}
	if len(durationList) != 1 || durationList[0].ID != overdue.ID {
		t.Fatalf("vencidas = %+v", durationList)
	}

	byStatus, err := repos.Calls().ListByStatus(ctx, domain.CallActive, 10)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != overdue.ID {
		t.Fatalf("activas = %+v", byStatus)
	}
}

func TestMemberRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	repos := postgres.NewStore(db.adapter).Repositories()

	owner := insertUser(t, ctx, db, "ana")
	invitee := insertUser(t, ctx, db, "beto")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_miembros",
		OwnerID:            owner,
		Status:             domain.CallCreated,
		CreatedAt:          now,
		JoinDeadline:       now.Add(10 * time.Minute),
		MaxDurationSeconds: 3600,
	}
	if err := repos.Calls().Insert(ctx, call); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	members := []domain.CallMember{
		{UserID: owner, Role: domain.RoleCreator, AllowedSources: domain.AllowedSourcesForRole(domain.RoleCreator), CreatedAt: now},
		{UserID: invitee, Role: domain.RoleViewer, AllowedSources: domain.AllowedSourcesForRole(domain.RoleViewer), CreatedAt: now},
	}
	if err := repos.Members().InsertMembers(ctx, call.ID, members); err != nil {
		t.Fatalf("InsertMembers: %v", err)
	}

	list, err := repos.Members().ListByCall(ctx, call.ID)
	if err != nil {
		t.Fatalf("ListByCall: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("miembros = %d, se esperaban 2", len(list))
	}

	member, err := repos.Members().Get(ctx, call.ID, invitee)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if member.Role != domain.RoleViewer {
		t.Fatalf("rol = %s, se esperaba viewer", member.Role)
	}
}

func TestSessionRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	repos := postgres.NewStore(db.adapter).Repositories()

	owner := insertUser(t, ctx, db, "ana")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_sesiones",
		OwnerID:            owner,
		Status:             domain.CallActive,
		CreatedAt:          now,
		JoinDeadline:       now.Add(10 * time.Minute),
		MaxDurationSeconds: 3600,
	}
	if err := repos.Calls().Insert(ctx, call); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	session := domain.CallSession{
		ID:             "33333333-3333-4333-8333-333333333333",
		CallID:         call.ID,
		UserID:         owner,
		ParticipantSID: "PA_1",
		RoomSID:        "RM_1",
		JoinedAt:       now,
	}
	if err := repos.Sessions().Insert(ctx, session); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	open, err := repos.Sessions().ListOpenByCall(ctx, call.ID)
	if err != nil {
		t.Fatalf("ListOpenByCall: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("sesiones abiertas = %d, se esperaba 1", len(open))
	}

	if err := repos.Sessions().Close(ctx, session.ID, now.Add(time.Minute), "desconexión"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	open, err = repos.Sessions().ListOpenByCall(ctx, call.ID)
	if err != nil {
		t.Fatalf("ListOpenByCall: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("sesiones abiertas = %d, se esperaba 0", len(open))
	}
}

func TestEventRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	repos := postgres.NewStore(db.adapter).Repositories()

	owner := insertUser(t, ctx, db, "ana")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_eventos",
		OwnerID:            owner,
		Status:             domain.CallCreated,
		CreatedAt:          now,
		JoinDeadline:       now.Add(10 * time.Minute),
		MaxDurationSeconds: 3600,
	}
	if err := repos.Calls().Insert(ctx, call); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	actor := owner
	event := domain.CallEvent{
		ID:         "44444444-4444-4444-8444-444444444444",
		CallID:     call.ID,
		EventType:  "call.created",
		ActorID:    &actor,
		OccurredAt: now,
		Details:    map[string]any{"invitee_role": "participant"},
	}
	if err := repos.Events().Append(ctx, event); err != nil {
		t.Fatalf("Append: %v", err)
	}

	provider := domain.CallEvent{
		ID:              "55555555-5555-4555-8555-555555555555",
		CallID:          call.ID,
		EventType:       "provider.event",
		OccurredAt:      now,
		ProviderEventID: "evt_1",
	}
	inserted, err := repos.Events().AppendProviderEvent(ctx, provider)
	if err != nil {
		t.Fatalf("AppendProviderEvent: %v", err)
	}
	if !inserted {
		t.Fatal("el primer evento del proveedor debe guardarse")
	}

	duplicated := provider
	duplicated.ID = "66666666-6666-4666-8666-666666666666"
	inserted, err = repos.Events().AppendProviderEvent(ctx, duplicated)
	if err != nil {
		t.Fatalf("AppendProviderEvent: %v", err)
	}
	if inserted {
		t.Fatal("un evento repetido del proveedor no debe duplicarse")
	}
}

func TestOutboxRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	repos := postgres.NewStore(db.adapter).Repositories()

	owner := insertUser(t, ctx, db, "ana")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_trabajos",
		OwnerID:            owner,
		Status:             domain.CallEnding,
		CreatedAt:          now,
		JoinDeadline:       now.Add(10 * time.Minute),
		MaxDurationSeconds: 3600,
	}
	if err := repos.Calls().Insert(ctx, call); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	job := domain.OutboxJob{
		ID:            "77777777-7777-4777-8777-777777777777",
		CallID:        call.ID,
		Operation:     domain.OperationDeleteRoom,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	if err := repos.Outbox().Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Repetir el encolado no duplica el trabajo pendiente.
	duplicate := job
	duplicate.ID = "88888888-8888-4888-8888-888888888888"
	if err := repos.Outbox().Enqueue(ctx, duplicate); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := repos.Outbox().ClaimDue(ctx, now, 10, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("trabajos = %d, se esperaba 1", len(claimed))
	}
	if claimed[0].Operation != domain.OperationDeleteRoom {
		t.Fatalf("trabajo = %+v", claimed[0])
	}

	if err := repos.Outbox().MarkFailed(ctx, job.ID, 1, now.Add(time.Minute), "falló la eliminación"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	pending, err := repos.Outbox().ClaimDue(ctx, now, 10, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("trabajos = %d, se esperaba 0 hasta el próximo intento", len(pending))
	}

	if err := repos.Outbox().MarkCompleted(ctx, job.ID, now); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
}

func TestUnitOfWorkRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newTestDatabase(t)
	store := postgres.NewStore(db.adapter)

	owner := insertUser(t, ctx, db, "ana")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	call := domain.Call{
		ID:                 domain.CallID("11111111-1111-4111-8111-111111111111"),
		RoomName:           "call_rollback",
		OwnerID:            owner,
		Status:             domain.CallCreated,
		CreatedAt:          now,
		JoinDeadline:       now.Add(10 * time.Minute),
		MaxDurationSeconds: 3600,
	}

	failure := errors.New("fallo simulado")
	err := store.UnitOfWork().WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		if err := repos.Calls().Insert(ctx, call); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, se esperaba el fallo simulado", err)
	}

	if _, err := store.Repositories().Calls().GetByID(ctx, call.ID); !errors.Is(err, ports.ErrCallNotFound) {
		t.Fatalf("error = %v, la llamada debía revertirse", err)
	}
}
