package httpadapter

import (
	"bytes"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"

	"webrtc/api/internal/application/ports"
)

const idempotencyKeyHeader = "Idempotency-Key"

type handlers struct {
	auth       ports.Auth
	users      ports.Users
	calls      ports.Calls
	webhooks   WebhookReceiver
	appBaseURL *url.URL
}

// POST /api/v1/auth/login
func (h *handlers) login(c fiber.Ctx) error {
	var req loginRequest
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := req.validate(); err != nil {
		return err
	}
	res, err := h.auth.Login(c.Context(), ports.LoginInput{Username: req.Username, Password: req.Password})
	if err != nil {
		return err
	}
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    res.SessionToken,
		Path:     "/",
		Expires:  res.Session.ExpiresAt.UTC(),
		Secure:   true,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
	return c.JSON(toSessionResponse(res.Session))
}

// POST /api/v1/auth/logout
func (h *handlers) logout(c fiber.Ctx) error {
	if err := h.auth.Logout(c.Context(), authFrom(c).token); err != nil {
		return err
	}
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		Secure:   true,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/v1/auth/me
func (h *handlers) me(c fiber.Ctx) error {
	return c.JSON(toSessionResponse(authFrom(c).session))
}

// GET /api/v1/users
func (h *handlers) listUsers(c fiber.Ctx) error {
	list, err := h.users.ListActiveUsers(c.Context(), authFrom(c).session.User.ID)
	if err != nil {
		return err
	}
	resp := usersResponse{Users: make([]userResponse, 0, len(list))}
	for _, u := range list {
		resp.Users = append(resp.Users, toUserResponse(u))
	}
	return c.JSON(resp)
}

// POST /api/v1/calls
func (h *handlers) createCall(c fiber.Ctx) error {
	key := c.Get(idempotencyKeyHeader)
	if key != "" && !validIdempotencyKey(key) {
		return errValidation("Idempotency-Key admite de 1 a 255 caracteres ASCII visibles")
	}
	var req createCallRequest
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	in, err := req.toInput(authFrom(c).session.User.ID, key)
	if err != nil {
		return err
	}
	res, err := h.calls.CreateCall(c.Context(), in)
	if err != nil {
		return err
	}
	if res.Replayed {
		c.Set("Idempotent-Replayed", "true")
	}
	c.Location("/api/v1/calls/" + string(res.CallID))
	return c.Status(fiber.StatusCreated).JSON(createCallResponse{
		CallID:  string(res.CallID),
		JoinURL: h.appBaseURL.JoinPath("calls", string(res.CallID)).String(),
		Status:  string(res.Status),
	})
}

// GET /api/v1/calls/:id
func (h *handlers) getCall(c fiber.Ctx) error {
	access, err := callAccess(c)
	if err != nil {
		return err
	}
	view, err := h.calls.GetCall(c.Context(), access)
	if err != nil {
		return err
	}
	return c.JSON(toCallResponse(view))
}

// POST /api/v1/calls/:id/join-token
func (h *handlers) issueJoinToken(c fiber.Ctx) error {
	access, err := callAccess(c)
	if err != nil {
		return err
	}
	creds, err := h.calls.IssueJoinToken(c.Context(), access)
	if err != nil {
		return err
	}
	return c.JSON(toJoinTokenResponse(creds))
}

// POST /api/v1/calls/:id/end
func (h *handlers) endCall(c fiber.Ctx) error {
	access, err := callAccess(c)
	if err != nil {
		return err
	}
	res, err := h.calls.EndCall(c.Context(), access)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(endCallResponse{Status: string(res.Status)})
}

// POST /api/v1/webhooks/livekit
func (h *handlers) receiveWebhook(c fiber.Ctx) error {
	// La firma se calcula sobre el cuerpo original: se entrega sin decodificar.
	if err := h.webhooks.Receive(c.Context(), bytes.Clone(c.Body()), c.Get(fiber.HeaderAuthorization)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// callAccess combina el actor de la sesión con el identificador de la ruta.
// La identidad nunca se toma del cuerpo ni de la URL.
func callAccess(c fiber.Ctx) (ports.CallAccess, error) {
	id, err := parseCallID(c.Params("id"))
	if err != nil {
		return ports.CallAccess{}, err
	}
	return ports.CallAccess{ActorID: authFrom(c).session.User.ID, CallID: id}, nil
}
