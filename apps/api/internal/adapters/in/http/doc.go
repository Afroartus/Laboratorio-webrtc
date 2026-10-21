// Package httpadapter contiene los handlers net/http y los DTO HTTP. Decodifica,
// valida formato, obtiene la sesión e invoca casos de uso; no contiene SQL,
// reglas de pertenencia ni llamadas al SDK.
//
// El directorio se llama http según la estructura del spec; el paquete se llama
// httpadapter para no ocultar net/http.
package httpadapter
