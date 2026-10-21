package main

import (
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"APP_BASE_URL":              "https://webrtc.test:8443",
		"ALLOWED_WEB_ORIGINS":       "https://webrtc.test:8443",
		"DATABASE_URL":              "postgres://webrtc:secret@postgres:5432/webrtc?sslmode=disable",
		"LIVEKIT_URL":               "wss://rtc.webrtc.test:8443",
		"LIVEKIT_API_URL":           "http://livekit:7880",
		"LIVEKIT_API_KEY":           "lab-key",
		"LIVEKIT_API_SECRET":        strings.Repeat("s", 32),
		"LAB_HOST_IP":               "127.0.0.1",
		"AUTH_MODE":                 "local",
		"CALL_MAX_DURATION_SECONDS": "3600",
		"JOIN_TOKEN_TTL_SECONDS":    "120",
	}
}

func getenvFrom(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestLoadConfigValid(t *testing.T) {
	env := validEnv()
	env["ALLOWED_WEB_ORIGINS"] = "https://webrtc.test:8443, https://192.168.1.10:8443/"

	cfg, err := loadConfig(getenvFrom(env))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, se esperaba :8080 por defecto", cfg.HTTPAddr)
	}
	if got := strings.Join(cfg.AllowedWebOrigins, " "); got != "https://webrtc.test:8443 https://192.168.1.10:8443" {
		t.Errorf("AllowedWebOrigins = %q", got)
	}
	if cfg.CallMaxDuration != time.Hour || cfg.JoinTokenTTL != 2*time.Minute {
		t.Errorf("duraciones = %v / %v", cfg.CallMaxDuration, cfg.JoinTokenTTL)
	}
}

func TestLoadConfigReportsAllMissing(t *testing.T) {
	_, err := loadConfig(getenvFrom(map[string]string{}))
	if err == nil {
		t.Fatal("se esperaba error con entorno vacío")
	}
	for name := range validEnv() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("el error no menciona %s:\n%v", name, err)
		}
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"APP_BASE_URL":              "http://webrtc.test:8443",
		"ALLOWED_WEB_ORIGINS":       "https://webrtc.test:8443/app",
		"DATABASE_URL":              "mysql://x",
		"LIVEKIT_URL":               "https://rtc.webrtc.test:8443",
		"LIVEKIT_API_SECRET":        "corto",
		"LAB_HOST_IP":               "0.0.0.0",
		"AUTH_MODE":                 "oidc",
		"CALL_MAX_DURATION_SECONDS": "-1",
		"JOIN_TOKEN_TTL_SECONDS":    "abc",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env[name] = value
			_, err := loadConfig(getenvFrom(env))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("se esperaba error para %s=%q, obtenido: %v", name, value, err)
			}
		})
	}
}

func TestLoadConfigErrorDoesNotLeakSecret(t *testing.T) {
	env := validEnv()
	env["LIVEKIT_API_SECRET"] = "secreto-corto"
	_, err := loadConfig(getenvFrom(env))
	if err == nil || strings.Contains(err.Error(), "secreto-corto") {
		t.Fatalf("el error no debe incluir el secreto: %v", err)
	}
}
