// Package usecases implementa los casos de uso: Login, Logout, CreateCall,
// GetCall, IssueJoinToken, EndCall, HandleMediaEvent y ReconcileCalls.
//
// Regla de dependencias: domain y application/ports. Recibe implementaciones
// por constructor; nunca importa adapters.
package usecases
