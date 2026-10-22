package livekit

import (
	"context"
	"errors"
	"fmt"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"

	"webrtc/api/internal/application/ports"
)

// roomService implementa el puerto RoomService con el SDK oficial de LiveKit
// para Go. Ningún tipo del SDK sale de este paquete.
type roomService struct {
	client *lksdk.RoomServiceClient
}

// NewRoomService devuelve el adaptador de salas del servidor de medios.
func NewRoomService(apiURL, apiKey, apiSecret string) ports.RoomService {
	return &roomService{client: lksdk.NewRoomServiceClient(apiURL, apiKey, apiSecret)}
}

// GetRoom devuelve la información de la sala indicada.
func (s *roomService) GetRoom(ctx context.Context, roomName string) (ports.RoomInfo, error) {
	response, err := s.client.ListRooms(ctx, &livekit.ListRoomsRequest{Names: []string{roomName}})
	if err != nil {
		return ports.RoomInfo{}, fmt.Errorf("no se pudo consultar la sala en el servidor de medios: %w", err)
	}
	for _, room := range response.Rooms {
		if room.Name != roomName {
			continue
		}
		return ports.RoomInfo{
			Name:         room.Name,
			SID:          room.Sid,
			Participants: int(room.NumParticipants),
		}, nil
	}
	return ports.RoomInfo{}, ports.ErrRoomNotFound
}

// DeleteRoom elimina la sala. Es idempotente: si la sala ya no existe, el
// servidor de medios responde con éxito.
func (s *roomService) DeleteRoom(ctx context.Context, roomName string) error {
	if _, err := s.client.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: roomName}); err != nil {
		var twirpErr twirp.Error
		if errors.As(err, &twirpErr) && twirpErr.Code() == twirp.NotFound {
			return nil
		}
		return fmt.Errorf("no se pudo eliminar la sala en el servidor de medios: %w", err)
	}
	return nil
}
