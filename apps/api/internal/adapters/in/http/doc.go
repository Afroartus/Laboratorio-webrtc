// Package httpadapter contiene los handlers Fiber y los DTO HTTP de la API
// (contrato en apps/api/api/openapi.yaml). Decodifica, valida formato, obtiene
// la sesión e invoca los puertos de entrada; no contiene SQL, reglas de
// pertenencia ni llamadas al SDK.
//
// El directorio se llama http según la estructura del spec; el paquete se llama
// httpadapter para no ocultar net/http.
package httpadapter
