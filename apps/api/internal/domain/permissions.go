package domain

// PermissionsForRole devuelve los permisos de publicación del rol indicado.
// El creador y el participante publican cámara, micrófono y pantalla; el
// espectador solo se suscribe.
func PermissionsForRole(role Role) Permissions {
	switch role {
	case RoleCreator, RoleParticipant:
		return Permissions{
			PublishCamera:     true,
			PublishMicrophone: true,
			PublishScreen:     true,
			Subscribe:         true,
		}
	case RoleViewer:
		return Permissions{Subscribe: true}
	default:
		return Permissions{}
	}
}

// AllowedSourcesForRole devuelve las fuentes que el rol puede publicar.
func AllowedSourcesForRole(role Role) []MediaSource {
	switch role {
	case RoleCreator, RoleParticipant:
		return []MediaSource{SourceCamera, SourceMicrophone, SourceScreen}
	default:
		return []MediaSource{}
	}
}

// IsValidRole indica si el rol pertenece al vocabulario de la aplicación.
func IsValidRole(role Role) bool {
	switch role {
	case RoleCreator, RoleParticipant, RoleViewer:
		return true
	default:
		return false
	}
}
