package domain

import "errors"

// Errores del dominio. Los adaptadores y los casos de uso los traducen a sus
// propios errores de aplicación; el núcleo no conoce códigos HTTP ni SQL.
var (
	// ErrInvalidTransition indica que la transición de estado no está permitida.
	ErrInvalidTransition = errors.New("transición de estado no permitida")
	// ErrInvalidCall indica que la entidad de llamada no cumple sus invariantes.
	ErrInvalidCall = errors.New("llamada inválida")
	// ErrInvalidMember indica que la pertenencia a la llamada no es válida.
	ErrInvalidMember = errors.New("miembro inválido")
)
