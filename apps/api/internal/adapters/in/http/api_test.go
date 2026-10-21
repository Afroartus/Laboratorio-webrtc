package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

const (
	testOrigin = "https://webrtc.test:8443"
	testCallID = "2f1c0a0e-6a52-4c39-8d0c-1f1b6c1c7d10"
)

type testSession struct {
	token string
	csrf  string
	user  ports.UserSummary
}

var (
	alice = testSession{token: "tok-alice", csrf: "csrf-alice", user: ports.UserSummary{ID: "u-alice", Username: "alice", DisplayName: "Alice"}}
	bob   = testSession{token: "tok-bob", csrf: "csrf-bob", user: ports.UserSummary{ID: "u-bob", Username: "bob", DisplayName: "Bob"}}
)

var sessionExpiry = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

type testEnv struct {
	app      *fiber.App
	auth     *fakeAuth
	users    *fakeUsers
	calls    *fakeCalls
	webhooks *fakeWebhooks
}

func newTestEnv(t *testing.T, opts ...func(*Config)) *testEnv {
	t.Helper()
	sessions := map[string]ports.Session{}
	for _, s := range []testSession{alice, bob} {
		sessions[s.token] = ports.Session{User: s.user, CSRFToken: s.csrf, ExpiresAt: sessionExpiry}
	}
	e := &testEnv{
		auth: &fakeAuth{
			sessions: sessions,
			login: func(in ports.LoginInput) (ports.LoginResult, error) {
				if in.Username != "alice" || in.Password != "correcta" {
					return ports.LoginResult{}, fmt.Errorf("bcrypt: hash mismatch: %w", ports.ErrInvalidCredentials)
				}
				return ports.LoginResult{SessionToken: "nuevo-token", Session: sessions[alice.token]}, nil
			},
		},
		users: &fakeUsers{users: []ports.UserSummary{alice.user, bob.user}},
		calls: &fakeCalls{
			create: func(ports.CreateCallInput) (ports.CreateCallResult, error) {
				return ports.CreateCallResult{CallID: testCallID, Status: domain.CallCreated}, nil
			},
			get: func(in ports.CallAccess) (ports.CallView, error) {
				return ports.CallView{
					CallID:       in.CallID,
					Status:       domain.CallActive,
					Role:         domain.RoleViewer,
					Permissions:  domain.Permissions{Subscribe: true},
					JoinDeadline: time.Date(2026, 10, 21, 10, 10, 0, 0, time.FixedZone("CET", 3600)),
				}, nil
			},
			joinToken: func(in ports.CallAccess) (ports.JoinCredentials, error) {
				return ports.JoinCredentials{
					CallID:           in.CallID,
					RoomName:         "call_" + string(in.CallID),
					ServerURL:        "wss://rtc.webrtc.test:8443",
					ParticipantToken: "jwt-" + string(in.ActorID),
					ExpiresAt:        time.Date(2026, 10, 21, 10, 2, 0, 0, time.UTC),
					Permissions:      domain.Permissions{PublishCamera: true, PublishMicrophone: true, PublishScreen: true, Subscribe: true},
				}, nil
			},
			end: func(in ports.CallAccess) (ports.EndCallResult, error) {
				return ports.EndCallResult{CallID: in.CallID, Status: domain.CallEnding}, nil
			},
		},
		webhooks: &fakeWebhooks{},
	}

	cfg := Config{
		AppBaseURL:     mustParseURL(t, testOrigin),
		AllowedOrigins: []string{testOrigin},
	}
	for _, o := range opts {
		o(&cfg)
	}
	logger := slog.New(slog.DiscardHandler)
	e.app = NewApp(logger)
	RegisterHealth(e.app, logger)
	if err := RegisterAPI(e.app, cfg, Dependencies{Auth: e.auth, Users: e.users, Calls: e.calls, Webhooks: e.webhooks}); err != nil {
		t.Fatal(err)
	}
	return e
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// browserRequest construye una solicitud como la enviaría la SPA: mismo
// origen, cookie de sesión y token CSRF cuando hay sesión.
func browserRequest(method, path, body string, s *testSession) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, path, r)
	req.Header.Set("Origin", testOrigin)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if s != nil {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: s.token})
		req.Header.Set(csrfHeader, s.csrf)
	}
	return req
}

func (e *testEnv) do(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := e.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("respuesta JSON inválida: %v", err)
	}
	return v
}

// expectError comprueba estado, código y formato común del error.
func expectError(t *testing.T, resp *http.Response, status int, code string) errorResponse {
	t.Helper()
	if resp.StatusCode != status {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("estado = %d, se esperaba %d; cuerpo: %s", resp.StatusCode, status, body)
	}
	got := decode[errorResponse](t, resp)
	if got.Code != code {
		t.Errorf("code = %q, se esperaba %q", got.Code, code)
	}
	if got.Message == "" {
		t.Error("message vacío")
	}
	if got.RequestID == "" || got.RequestID != resp.Header.Get(requestIDHeader) {
		t.Errorf("requestId = %q, X-Request-ID = %q", got.RequestID, resp.Header.Get(requestIDHeader))
	}
	return got
}

func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("sin cookie %s; Set-Cookie: %v", sessionCookieName, resp.Header.Values("Set-Cookie"))
	return nil
}

// --- Autenticación ---------------------------------------------------------

func TestLoginSetsHardenedSessionCookie(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("POST", "/api/v1/auth/login", `{"username":"alice","password":"correcta"}`, nil))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	c := sessionCookie(t, resp)
	if c.Value != "nuevo-token" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
		t.Errorf("cookie insegura o incorrecta: %+v", c)
	}
	if !c.Expires.Equal(sessionExpiry) {
		t.Errorf("Expires = %v, se esperaba %v", c.Expires, sessionExpiry)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	body := decode[sessionResponse](t, resp)
	if body.CSRFToken != alice.csrf || body.User.Username != "alice" || !body.ExpiresAt.Equal(sessionExpiry) {
		t.Errorf("cuerpo = %+v", body)
	}
}

func TestLoginRejectsBeforeReachingUseCase(t *testing.T) {
	formReq := browserRequest("POST", "/api/v1/auth/login", "username=alice&password=x", nil)
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	noOrigin := browserRequest("POST", "/api/v1/auth/login", `{"username":"alice","password":"correcta"}`, nil)
	noOrigin.Header.Del("Origin")
	foreign := browserRequest("POST", "/api/v1/auth/login", `{"username":"alice","password":"correcta"}`, nil)
	foreign.Header.Set("Origin", "https://evil.test")

	cases := []struct {
		name   string
		req    *http.Request
		status int
		code   string
	}{
		{"sin Origin", noOrigin, 403, codeOriginNotAllowed},
		{"Origin ajeno", foreign, 403, codeOriginNotAllowed},
		{"formulario (login CSRF)", formReq, 415, codeUnsupportedMediaType},
		{"JSON inválido", browserRequest("POST", "/api/v1/auth/login", `{"username":`, nil), 400, codeValidation},
		{"campo desconocido", browserRequest("POST", "/api/v1/auth/login", `{"username":"a","password":"b","admin":true}`, nil), 400, codeValidation},
		{"datos sobrantes", browserRequest("POST", "/api/v1/auth/login", `{"username":"a","password":"b"}{}`, nil), 400, codeValidation},
		{"usuario vacío", browserRequest("POST", "/api/v1/auth/login", `{"username":"","password":"b"}`, nil), 400, codeValidation},
		{"contraseña larga", browserRequest("POST", "/api/v1/auth/login", `{"username":"a","password":"`+strings.Repeat("x", 257)+`"}`, nil), 400, codeValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			expectError(t, e.do(t, tc.req), tc.status, tc.code)
			if n := e.auth.LoginCalls(); n != 0 {
				t.Errorf("Login invocado %d veces", n)
			}
		})
	}
}

func TestLoginInvalidCredentialsDoesNotLeakDetails(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("POST", "/api/v1/auth/login", `{"username":"alice","password":"mala"}`, nil))
	got := expectError(t, resp, 401, codeInvalidCredentials)
	if strings.Contains(got.Message, "bcrypt") {
		t.Errorf("el mensaje filtra el error interno: %q", got.Message)
	}
	if len(resp.Cookies()) != 0 {
		t.Error("no debe establecer cookie si el login falla")
	}
}

func TestLoginIsRateLimitedPerClientIP(t *testing.T) {
	e := newTestEnv(t, func(c *Config) { c.LoginRateLimit = 3 })
	for i := range 3 {
		resp := e.do(t, browserRequest("POST", "/api/v1/auth/login", `{"username":"alice","password":"mala"}`, nil))
		if resp.StatusCode != 401 {
			t.Fatalf("intento %d: estado %d", i+1, resp.StatusCode)
		}
	}
	resp := e.do(t, browserRequest("POST", "/api/v1/auth/login", `{"username":"alice","password":"correcta"}`, nil))
	expectError(t, resp, 429, codeTooManyRequests)
	if resp.Header.Get("Retry-After") == "" {
		t.Error("falta Retry-After")
	}
	if n := e.auth.LoginCalls(); n != 3 {
		t.Errorf("Login invocado %d veces, se esperaban 3", n)
	}
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("POST", "/api/v1/auth/logout", "", &alice))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	if !slices.Equal(e.auth.logoutTokens, []string{alice.token}) {
		t.Errorf("Logout recibió %v", e.auth.logoutTokens)
	}
	c := sessionCookie(t, resp)
	if c.Value != "" || c.MaxAge >= 0 || !c.Secure || !c.HttpOnly {
		t.Errorf("la cookie no se elimina correctamente: %+v", c)
	}
}

func TestLogoutKeepsCookieWhenRevocationFails(t *testing.T) {
	e := newTestEnv(t)
	e.auth.logoutErr = fmt.Errorf("pool cerrado: %w", ports.ErrUnavailable)
	resp := e.do(t, browserRequest("POST", "/api/v1/auth/logout", "", &alice))
	expectError(t, resp, 503, codeUnavailable)
	if len(resp.Cookies()) != 0 {
		t.Error("no debe borrar la cookie si la sesión sigue vigente en el servidor")
	}
}

func TestMeReturnsCurrentUserAndCSRF(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("GET", "/api/v1/auth/me", "", &bob))
	if resp.StatusCode != 200 {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	body := decode[sessionResponse](t, resp)
	if body.User.ID != "u-bob" || body.CSRFToken != bob.csrf {
		t.Errorf("cuerpo = %+v", body)
	}
}

func TestListUsersExposesOnlyPublicFields(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("GET", "/api/v1/users", "", &alice))
	if resp.StatusCode != 200 {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	body := decode[map[string][]map[string]any](t, resp)
	if len(body["users"]) != 2 {
		t.Fatalf("users = %v", body)
	}
	for _, u := range body["users"] {
		if keys := slices.Sorted(maps.Keys(u)); !slices.Equal(keys, []string{"displayName", "id", "username"}) {
			t.Errorf("campos expuestos = %v", keys)
		}
	}
	if !slices.Equal(e.users.actors, []domain.UserID{"u-alice"}) {
		t.Errorf("actor = %v", e.users.actors)
	}
}

// --- Autorización transversal -----------------------------------------------

type route struct{ method, path, body string }

var sessionRoutes = []route{
	{"POST", "/api/v1/auth/logout", ""},
	{"GET", "/api/v1/auth/me", ""},
	{"GET", "/api/v1/users", ""},
	{"POST", "/api/v1/calls", `{"inviteeUserId":"u-bob","inviteeRole":"participant"}`},
	{"GET", "/api/v1/calls/" + testCallID, ""},
	{"POST", "/api/v1/calls/" + testCallID + "/join-token", ""},
	{"POST", "/api/v1/calls/" + testCallID + "/end", ""},
}

func TestProtectedRoutesRequireValidSession(t *testing.T) {
	expired := testSession{token: "revocado", csrf: "x"}
	for _, r := range sessionRoutes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			e := newTestEnv(t)
			expectError(t, e.do(t, browserRequest(r.method, r.path, r.body, nil)), 401, codeUnauthenticated)
			expectError(t, e.do(t, browserRequest(r.method, r.path, r.body, &expired)), 401, codeUnauthenticated)
			if e.calls.Invoked() != 0 || len(e.users.actors) != 0 || len(e.auth.logoutTokens) != 0 {
				t.Error("se invocó un caso de uso sin sesión válida")
			}
		})
	}
}

func TestMutationsRequireCSRFAndAllowedOrigin(t *testing.T) {
	for _, r := range sessionRoutes {
		if r.method != "POST" {
			continue
		}
		t.Run(r.path, func(t *testing.T) {
			e := newTestEnv(t)

			missing := browserRequest(r.method, r.path, r.body, &alice)
			missing.Header.Del(csrfHeader)
			expectError(t, e.do(t, missing), 403, codeCSRFInvalid)

			// El token CSRF de otra sesión no sirve.
			wrong := browserRequest(r.method, r.path, r.body, &alice)
			wrong.Header.Set(csrfHeader, bob.csrf)
			expectError(t, e.do(t, wrong), 403, codeCSRFInvalid)

			foreign := browserRequest(r.method, r.path, r.body, &alice)
			foreign.Header.Set("Origin", "https://webrtc.test:9443")
			expectError(t, e.do(t, foreign), 403, codeOriginNotAllowed)

			if e.calls.Invoked() != 0 || len(e.auth.logoutTokens) != 0 {
				t.Error("se invocó un caso de uso sin CSRF u origen válidos")
			}
		})
	}
}

// --- Llamadas ----------------------------------------------------------------

func TestCreateCallUsesSessionIdentity(t *testing.T) {
	e := newTestEnv(t)
	var got ports.CreateCallInput
	e.calls.create = func(in ports.CreateCallInput) (ports.CreateCallResult, error) {
		got = in
		return ports.CreateCallResult{CallID: testCallID, Status: domain.CallCreated}, nil
	}
	req := browserRequest("POST", "/api/v1/calls", `{"inviteeUserId":"u-bob","inviteeRole":"viewer"}`, &alice)
	req.Header.Set("Idempotency-Key", "crear-1")
	resp := e.do(t, req)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	want := ports.CreateCallInput{ActorID: "u-alice", InviteeID: "u-bob", InviteeRole: domain.RoleViewer, IdempotencyKey: "crear-1"}
	if got != want {
		t.Errorf("entrada = %+v, se esperaba %+v", got, want)
	}
	if loc := resp.Header.Get("Location"); loc != "/api/v1/calls/"+testCallID {
		t.Errorf("Location = %q", loc)
	}
	if resp.Header.Get("Idempotent-Replayed") != "" {
		t.Error("una creación nueva no es una repetición")
	}
	body := decode[createCallResponse](t, resp)
	want2 := createCallResponse{CallID: testCallID, JoinURL: testOrigin + "/calls/" + testCallID, Status: "CREATED"}
	if body != want2 {
		t.Errorf("cuerpo = %+v", body)
	}
}

func TestCreateCallValidation(t *testing.T) {
	cases := map[string]struct {
		body, key string
	}{
		"rol creator":                 {`{"inviteeUserId":"u-bob","inviteeRole":"creator"}`, ""},
		"rol desconocido":             {`{"inviteeUserId":"u-bob","inviteeRole":"admin"}`, ""},
		"sin invitado":                {`{"inviteeRole":"participant"}`, ""},
		"identidad en el cuerpo":      {`{"inviteeUserId":"u-bob","inviteeRole":"participant","ownerId":"u-bob"}`, ""},
		"Idempotency-Key con espacio": {`{"inviteeUserId":"u-bob","inviteeRole":"participant"}`, "a b"},
		"Idempotency-Key larga":       {`{"inviteeUserId":"u-bob","inviteeRole":"participant"}`, strings.Repeat("k", 256)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			req := browserRequest("POST", "/api/v1/calls", tc.body, &alice)
			if tc.key != "" {
				req.Header.Set("Idempotency-Key", tc.key)
			}
			expectError(t, e.do(t, req), 400, codeValidation)
			if e.calls.Invoked() != 0 {
				t.Error("CreateCall no debe invocarse")
			}
		})
	}
}

// idempotentCalls simula el contrato de idempotencia del caso de uso para
// comprobar que el adaptador lo transmite intacto bajo concurrencia.
type idempotentCalls struct {
	mu     sync.Mutex
	byKey  map[string]ports.CreateCallInput
	nextID int
}

func (s *idempotentCalls) create(in ports.CreateCallInput) (ports.CreateCallResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.byKey[in.IdempotencyKey]; ok {
		if prev != in {
			return ports.CreateCallResult{}, ports.ErrIdempotencyConflict
		}
		return ports.CreateCallResult{CallID: testCallID, Status: domain.CallCreated, Replayed: true}, nil
	}
	s.byKey[in.IdempotencyKey] = in
	s.nextID++
	return ports.CreateCallResult{CallID: testCallID, Status: domain.CallCreated}, nil
}

func TestCreateCallIdempotencyUnderConcurrency(t *testing.T) {
	e := newTestEnv(t)
	store := &idempotentCalls{byKey: map[string]ports.CreateCallInput{}}
	e.calls.create = store.create

	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	replayed, created := 0, 0
	for range n {
		wg.Go(func() {
			req := browserRequest("POST", "/api/v1/calls", `{"inviteeUserId":"u-bob","inviteeRole":"participant"}`, &alice)
			req.Header.Set("Idempotency-Key", "misma-clave")
			resp, err := e.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				t.Errorf("estado = %d", resp.StatusCode)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if resp.Header.Get("Idempotent-Replayed") == "true" {
				replayed++
			} else {
				created++
			}
		})
	}
	wg.Wait()
	if created != 1 || replayed != n-1 || store.nextID != 1 {
		t.Errorf("creadas = %d, repetidas = %d, llamadas en el almacén = %d", created, replayed, store.nextID)
	}

	// La misma clave con otro cuerpo es un conflicto.
	req := browserRequest("POST", "/api/v1/calls", `{"inviteeUserId":"u-bob","inviteeRole":"viewer"}`, &alice)
	req.Header.Set("Idempotency-Key", "misma-clave")
	expectError(t, e.do(t, req), 409, codeIdempotencyConflict)
}

func TestGetCallReturnsMemberView(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("GET", "/api/v1/calls/"+strings.ToUpper(testCallID), "", &bob))
	if resp.StatusCode != 200 {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	body := decode[callResponse](t, resp)
	if body.CallID != testCallID {
		t.Errorf("el identificador no se normaliza: %q", body.CallID)
	}
	if body.Role != "viewer" || body.Status != "ACTIVE" || body.Permissions != (permissionsResponse{Subscribe: true}) {
		t.Errorf("cuerpo = %+v", body)
	}
	if body.JoinDeadline.Location() != time.UTC || !body.JoinDeadline.Equal(time.Date(2026, 10, 21, 9, 10, 0, 0, time.UTC)) {
		t.Errorf("joinDeadline debe ir en UTC: %v", body.JoinDeadline)
	}
}

func TestMalformedCallIDIsNotVisible(t *testing.T) {
	for _, id := range []string{"123", "2f1c0a0e6a524c398d0c1f1b6c1c7d10xxxx", "{2f1c0a0e-6a52-4c39-8d0c-1f1b6c1c7d1}", "2f1c0a0e-6a52-4c39-8d0c-1f1b6c1c7d1g"} {
		e := newTestEnv(t)
		expectError(t, e.do(t, browserRequest("GET", "/api/v1/calls/"+url.PathEscape(id), "", &alice)), 404, codeCallNotFound)
		expectError(t, e.do(t, browserRequest("POST", "/api/v1/calls/"+url.PathEscape(id)+"/join-token", "", &alice)), 404, codeCallNotFound)
		if e.calls.Invoked() != 0 {
			t.Errorf("%q llegó al caso de uso", id)
		}
	}
}

func TestJoinTokenContract(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, browserRequest("POST", "/api/v1/calls/"+testCallID+"/join-token", "", &alice))
	if resp.StatusCode != 200 {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	body := decode[map[string]any](t, resp)
	wantKeys := []string{"callId", "expiresAt", "participantToken", "permissions", "roomName", "serverUrl"}
	if keys := slices.Sorted(maps.Keys(body)); !slices.Equal(keys, wantKeys) {
		t.Errorf("campos = %v, se esperaban %v", keys, wantKeys)
	}
	perms, _ := body["permissions"].(map[string]any)
	if keys := slices.Sorted(maps.Keys(perms)); !slices.Equal(keys, []string{"publishCamera", "publishMicrophone", "publishScreen", "subscribe"}) {
		t.Errorf("permisos = %v", keys)
	}
	if body["participantToken"] != "jwt-u-alice" || body["expiresAt"] != "2026-10-21T10:02:00Z" {
		t.Errorf("cuerpo = %v", body)
	}
}

func TestJoinTokenIsIssuedPerSessionUnderConcurrency(t *testing.T) {
	e := newTestEnv(t)
	var wg sync.WaitGroup
	for i := range 40 {
		s := alice
		if i%2 == 1 {
			s = bob
		}
		wg.Go(func() {
			resp, err := e.app.Test(browserRequest("POST", "/api/v1/calls/"+testCallID+"/join-token", "", &s), fiber.TestConfig{Timeout: 5 * time.Second})
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			var body joinTokenResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if want := "jwt-" + string(s.user.ID); body.ParticipantToken != want {
				t.Errorf("token = %q, se esperaba %q: mezcla de sesiones", body.ParticipantToken, want)
			}
		})
	}
	wg.Wait()
	if n := e.calls.Invoked(); n != 40 {
		t.Errorf("IssueJoinToken invocado %d veces; debe revalidar cada solicitud", n)
	}
}

func TestEndCallReturnsCurrentStatus(t *testing.T) {
	e := newTestEnv(t)
	statuses := []domain.CallStatus{domain.CallEnding, domain.CallEnded}
	e.calls.end = func(in ports.CallAccess) (ports.EndCallResult, error) {
		s := statuses[0]
		statuses = statuses[1:]
		return ports.EndCallResult{CallID: in.CallID, Status: s}, nil
	}
	for _, want := range []string{"ENDING", "ENDED"} {
		resp := e.do(t, browserRequest("POST", "/api/v1/calls/"+testCallID+"/end", "", &alice))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("estado = %d", resp.StatusCode)
		}
		if body := decode[endCallResponse](t, resp); body.Status != want {
			t.Errorf("status = %q, se esperaba %q", body.Status, want)
		}
	}
}

// --- Traducción de errores ---------------------------------------------------

func TestApplicationErrorsMapToContract(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ports.ErrInvalidInput, 400, codeValidation},
		{ports.ErrUnauthenticated, 401, codeUnauthenticated},
		{ports.ErrForbidden, 403, codeForbidden},
		{ports.ErrCallNotFound, 404, codeCallNotFound},
		{ports.ErrCallStateConflict, 409, codeCallStateConflict},
		{ports.ErrIdempotencyConflict, 409, codeIdempotencyConflict},
		{ports.ErrCallEnded, 410, codeCallEnded},
		{ports.ErrTooManyAttempts, 429, codeTooManyRequests},
		{ports.ErrUnavailable, 503, codeUnavailable},
		{errors.New("pgx: conexión rechazada a postgres:5432 password=secreto"), 500, codeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			e := newTestEnv(t)
			// Los casos de uso envuelven los errores con contexto técnico.
			wrapped := fmt.Errorf("IssueJoinToken: password=secreto: %w", tc.err)
			e.calls.joinToken = func(ports.CallAccess) (ports.JoinCredentials, error) { return ports.JoinCredentials{}, wrapped }
			got := expectError(t, e.do(t, browserRequest("POST", "/api/v1/calls/"+testCallID+"/join-token", "", &alice)), tc.status, tc.code)
			if strings.Contains(got.Message, "secreto") {
				t.Errorf("el mensaje filtra detalles internos: %q", got.Message)
			}
		})
	}
}

func TestPanicBecomesInternalError(t *testing.T) {
	e := newTestEnv(t)
	e.calls.get = func(ports.CallAccess) (ports.CallView, error) { panic("fallo inesperado") }
	expectError(t, e.do(t, browserRequest("GET", "/api/v1/calls/"+testCallID, "", &alice)), 500, codeInternal)
}

func TestUnknownRoute(t *testing.T) {
	e := newTestEnv(t)
	expectError(t, e.do(t, browserRequest("GET", "/api/v1/nada", "", &alice)), 404, codeNotFound)
}

// El límite de cuerpo lo aplica fasthttp antes del enrutado; app.Test no lo
// reproduce, así que se usa un servidor real en loopback.
func TestLargeBodyIsRejected(t *testing.T) {
	e := newTestEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = e.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = e.app.Shutdown() })

	big := `{"username":"` + strings.Repeat("a", defaultBodyLimit) + `","password":"x"}`
	req := browserRequest("POST", "http://"+ln.Addr().String()+"/api/v1/auth/login", big, nil)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	expectError(t, resp, 413, codePayloadTooLarge)
	if n := e.auth.LoginCalls(); n != 0 {
		t.Errorf("Login invocado %d veces", n)
	}
}

// --- Webhooks ----------------------------------------------------------------

func TestWebhookPassesRawBodyAndSignature(t *testing.T) {
	e := newTestEnv(t)
	raw := `{"event":"room_started", "id":"EV_1"}`
	req, _ := http.NewRequest("POST", "/api/v1/webhooks/livekit", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/webhook+json")
	req.Header.Set("Authorization", "firma-jwt")
	resp := e.do(t, req)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	if string(e.webhooks.body) != raw || e.webhooks.authorization != "firma-jwt" {
		t.Errorf("recibido body=%q authorization=%q", e.webhooks.body, e.webhooks.authorization)
	}
}

func TestWebhookErrors(t *testing.T) {
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		fmt.Errorf("firma: %w", ports.ErrUnauthenticated): {401, codeUnauthenticated},
		fmt.Errorf("persistir: %w", ports.ErrUnavailable): {503, codeUnavailable},
	} {
		e := newTestEnv(t)
		e.webhooks.err = err
		req, _ := http.NewRequest("POST", "/api/v1/webhooks/livekit", strings.NewReader(`{}`))
		expectError(t, e.do(t, req), want.status, want.code)
	}
}

// --- Salud -------------------------------------------------------------------

func TestHealthEndpoints(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	app := NewApp(logger)
	RegisterHealth(app, logger,
		ReadinessCheck{Name: "database", Check: func(ctx context.Context) error { return nil }},
		ReadinessCheck{Name: "livekit", Check: func(ctx context.Context) error { return errors.New("dial livekit:7880 secret=x") }},
	)

	resp, err := app.Test(httptestRequest("GET", "/health/live"))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("live: %v %v", resp, err)
	}

	resp, err = app.Test(httptestRequest("GET", "/health/ready"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	var body healthResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "not_ready" || body.Checks["database"] != "up" || body.Checks["livekit"] != "down" {
		t.Errorf("cuerpo = %s", raw)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "7880") {
		t.Errorf("la sonda revela detalles: %s", raw)
	}
}

func httptestRequest(method, path string) *http.Request {
	req, _ := http.NewRequest(method, path, nil)
	return req
}

func TestRegisterAPIRequiresDependencies(t *testing.T) {
	app := NewApp(slog.New(slog.DiscardHandler))
	cfg := Config{AppBaseURL: mustParseURL(t, testOrigin), AllowedOrigins: []string{testOrigin}}
	if err := RegisterAPI(app, cfg, Dependencies{}); err == nil {
		t.Error("se esperaba error sin dependencias")
	}
}
