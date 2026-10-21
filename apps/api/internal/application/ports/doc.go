// Package ports define los contratos de entrada y salida del núcleo
// (repositorios, UnitOfWork, RoomService, JoinTokenIssuer, PasswordHasher,
// SessionTokenGenerator, Clock).
//
// Regla de dependencias: domain y biblioteca estándar (context incluido). No
// expone tipos de net/http, pgx ni del SDK de LiveKit.
package ports
