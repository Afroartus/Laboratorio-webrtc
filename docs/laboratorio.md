# Guía del laboratorio local

Estado: paso 4. Construye y arranca `web`, `api`, `postgres` y `livekit`. Login, migraciones con contenido, usuarios de prueba y llamadas llegan en los pasos 5–9. La guía completa de entrega es el paso 13.

## Versiones fijadas

| Herramienta / imagen | Versión |
| --- | --- |
| Go (local y `golang` de build) | 1.26.3 |
| Node (local y `node` de build) | 24.18.0 (`node:24.18.0-alpine3.24`) |
| Svelte / Vite / TypeScript | 5.57.2 / 8.3.5 / 6.0.3 (exactas en `package.json` + `package-lock.json`) |
| Podman | 6.0.2 (máquina `podman-machine-default`, WSL, rootful) |
| Proveedor Compose | `podman-compose` 1.6.0 |
| Caddy | `caddy:2.11.7-alpine` |
| PostgreSQL | `postgres:18.6-alpine3.24` |
| LiveKit | `livekit/livekit-server:v1.13.10` |
| golang-migrate | `migrate/migrate:v4.20.1` |
| Base de ejecución API | `gcr.io/distroless/static-debian13:nonroot@sha256:d4df213e…a4219` |

## Requisitos

1. Podman con máquina iniciada (Windows): `podman machine list`; `init` solo si no existe, `start` solo si está detenida.
2. Proveedor Compose explícito:

   ```powershell
   python -m pip install --user podman-compose==1.6.0
   # pip lo instala en el directorio de scripts de usuario, que puede no estar en PATH:
   $env:PODMAN_COMPOSE_PROVIDER = "$env:APPDATA\Python\Python314\Scripts\podman-compose.exe"
   podman compose version   # debe indicar podman-compose version 1.6.0
   ```

## Configuración

```powershell
Copy-Item infra/.env.example infra/.env
podman run --rm docker.io/livekit/livekit-server:v1.13.10 generate-keys
```

Completar en `infra/.env`: `POSTGRES_PASSWORD` (alfanumérica), `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` (≥ 32 caracteres) y los valores del perfil. Si falta un valor obligatorio, `podman compose` se detiene con `required variable … is missing a value`; si la API recibe un valor inválido, termina con código 2 y lista todos los problemas.

### Perfil «mismo equipo»

- `LAB_HOST_IP=127.0.0.1`
- Archivo hosts del equipo: `127.0.0.1 webrtc.test rtc.webrtc.test`
- Dos navegadores o perfiles independientes en `https://webrtc.test:8443`.

### Perfil «LAN»

- `LAB_HOST_IP=<IP LAN del servidor>` y `ALLOWED_WEB_ORIGINS=https://webrtc.test:8443`
- Hosts/DNS local de cada equipo: `<IP LAN> webrtc.test rtc.webrtc.test`
- Firewall: permitir solo la subred del laboratorio hacia TCP 8443, TCP 7881 y UDP 7882.
- Instalar la raíz pública de la CA en cada cliente (ver abajo).

Los puertos se publican únicamente en `LAB_HOST_IP`: 8443/tcp (Caddy), 7881/tcp y 7882/udp (medios LiveKit). La API (8080), la señalización interna de LiveKit (7880) y PostgreSQL (5432) no se publican.

## Arranque

```powershell
$env:PODMAN_COMPOSE_PROVIDER = "$env:APPDATA\Python\Python314\Scripts\podman-compose.exe"
podman compose --env-file infra/.env -f infra/compose.yaml build
podman compose --env-file infra/.env -f infra/compose.yaml up -d postgres
# Tareas de una sola ejecución (perfil "tasks"). Las migraciones se crean en el paso 6:
# podman compose --env-file infra/.env -f infra/compose.yaml run --rm migrate
podman compose --env-file infra/.env -f infra/compose.yaml up -d
podman compose --env-file infra/.env -f infra/compose.yaml ps
podman compose --env-file infra/.env -f infra/compose.yaml logs api livekit
```

La build de la API ejecuta `go vet` y `go test`; la de la web, `svelte-check` y `tsc`. Si fallan, la imagen no se genera.

## CA local de Caddy

Caddy crea su CA en el volumen `webrtc-lab_caddy_data`. Exportar solo la raíz pública:

```powershell
podman cp webrtc-lab_web_1:/data/caddy/pki/authorities/local/root.crt ./lab-root.crt
```

Instalarla en «Entidades de certificación raíz de confianza» del usuario en cada cliente de prueba (en Windows: `certutil -user -addstore Root lab-root.crt`). Para retirarla, eliminarla del mismo almacén. `*.crt` está excluido de git. La clave privada de la CA no sale del volumen.

## Parada y datos

```powershell
podman compose --env-file infra/.env -f infra/compose.yaml down      # conserva volúmenes
```

Eliminar datos es una acción separada y explícita: `down -v` borra `postgres_data`, `caddy_data` (incluida la CA; habría que reinstalar la nueva raíz en los clientes) y `caddy_config`.

## Verificación realizada en el paso 4 (21-10-2026)

Equipo Windows 11 Pro, Podman 6.0.2, máquina WSL rootful, `user-mode-networking=false`, perfil «mismo equipo».

| Comprobación | Resultado |
| --- | --- |
| `podman compose config` con `.env` completo / sin secretos | Interpolación correcta / detención con mensaje claro. |
| `build` de `api` y `web` | Correcto (tests y checks dentro de la build). |
| `up -d` | `postgres` y `api` en `healthy`; `livekit` anuncia `nodeIP 127.0.0.1` sin STUN externo; Caddy emite certificados locales para ambos nombres. |
| HTTPS desde dentro de la VM, validando con la CA local | SPA 200, fallback `/calls/abc` → `index.html`, `/api/*` llega al servidor Go, `rtc.webrtc.test` llega a LiveKit, cabeceras HSTS y `nosniff`. |
| API sin configuración | Termina con código 2 y lista de variables faltantes/ inválidas, sin mostrar secretos. |
| **Acceso desde Windows a `127.0.0.1:8443`** | **Falla.** Ver hallazgo. |

### Hallazgo: reenvío de puertos Windows → máquina Podman

En esta máquina (WSL, rootful, sin user-mode networking) netavark publica los puertos con reglas DNAT de nftables dentro de la VM, sin un socket en escucha. El relé de localhost de WSL solo reenvía puertos con socket en escucha, así que ningún puerto publicado llega a Windows: tampoco un contenedor de prueba en `0.0.0.0`, ni el contenedor preexistente `webrtc` en `0.0.0.0:8080`. Los servicios funcionan dentro de la VM.

No es un defecto del Compose, pero bloquea los dos perfiles en este host. Se resolverá en el paso 10 (ruta navegador → firewall → red de Podman Machine → contenedor, incluido UDP 7882). Opciones a evaluar entonces, que modifican la máquina y requieren aprobación:

1. `podman machine set --user-mode-networking=true` (reenvío por gvproxy); comprobar UDP y la publicación ligada a una IP concreta.
2. Máquina en modo rootless.
3. Ejecutar los mismos contenedores en un equipo/VM Linux de la LAN.
